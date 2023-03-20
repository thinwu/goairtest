package adb

import (
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const (
	IP_PORT_REGEXP           = `(\d+\.\d+\.\d+\.\d+):\d+`
	DEVICE_LIST_TITLE_LINE   = "List of devices attached"
	DEVICE_SUFFIX_CONNECTED  = "device"
	DEVICE_INET_REGEXP       = `(inet\s)(\d+\.\d+\.\d+\.\d+)`
	DEVICE_TCPIP_CONNECTED   = "connected to"
	TCPIP_ALLREADY_CONNECTED = "already connected to"
	DEVICE_SUFFIX_OFFLINE    = "offline"
	DEVICE_AWAKE             = "interactiveState=INTERACTIVE_STATE_AWAKE"
	DEVICE_ASLEEP            = "interactiveState=INTERACTIVE_STATE_SLEEP"
	DEVICE_LOCKED            = "mInputRestricted=true"
	DEVICE_UNLOCKED          = "mInputRestricted=false"
)

type Message struct {
	Cmd    string
	Return string
}
type Device struct {
	IP                   string
	Port                 string
	SN                   string `json:"sn"`
	Password             string `json:"password"`
	WirelessConnected    bool
	WireConnected        bool
	WireRefreshToken     int64
	WirelessRefreshToken int64
	Invalid              bool
	Idle                 bool
}

func isIPPortValid(ip_port string) (string, bool) {
	re := regexp.MustCompile(IP_PORT_REGEXP)
	ret := re.FindString(ip_port)
	if ret == "" {
		return "", false
	} else {
		return ret, true
	}
}

func (d *Device) IP_Port() string {
	return fmt.Sprintf("%s:%s", d.IP, d.Port)
}
func newDevice() *Device {
	return &Device{}
}

type ADBServer struct {
	Devices []Device
}

func adbCmd(args ...string) []byte {
	cmd := exec.Command("adb", args...)
	out, err := cmd.Output()
	log.Printf("adbCmd: %v ", string(out))
	if err != nil {
		log.Printf("err adbCmd: %v \n\t%v", args, err)
		return nil
	}
	return out
}

func (d *Device) adbCmd(args ...string) []byte {
	var cmd = append([]string{"-s", d.SN}, args...)
	return adbCmd(cmd...)
}
func (d *Device) StopApp(cablePriority bool, appName string) []byte {
	return d.adbShellCmd(cablePriority, "am", "force-stop", appName)
}

func (d *Device) StartApp(cablePriority bool, appName string) []byte {
	return d.adbShellCmd(cablePriority, "monkey", "-p", appName, "1")
}

// cablePriority true: use sn connection first
func (d *Device) InstallAPK(cablePriority bool, apk string) []byte {
	device := d.SN
	if !cablePriority {
		ip_port, valid := isIPPortValid(d.IP_Port())
		if valid {
			device = ip_port
		}
	}
	return adbCmd("-s", device, "install", "-r", apk)
}

// cablePriority true: using SN, false: try using tcpip, args ...string
func (d *Device) adbShellCmd(cablePriority bool, args ...string) []byte {
	device := d.SN
	if !cablePriority {
		ip_port, valid := isIPPortValid(d.IP_Port())
		if valid {
			device = ip_port
		}
	}
	var cmd = append([]string{"-s", device, "shell"}, args...)
	return adbCmd(cmd...)
}

func (d *Device) openDeviceDefaultPort() {
	port := "5555"
	d.openDevicePort(port)
}

func (d *Device) openDevicePort(port string) []byte {
	d.getIP()
	var b = d.adbCmd("tcpip", port)
	d.Port = port
	return b
}
func (d *Device) disconnect() []byte {
	return adbCmd("disconnect", d.IP_Port())
}
func (d *Device) getSerialNO() {
	out := d.adbShellCmd(false, "getprop", "ro.boot.serialno")
	d.SN = strings.TrimSpace(string(out))
}
func (d *Device) getIP() {
	out := d.adbShellCmd(true, "ip", "-f", "inet", "addr", "show")
	re := regexp.MustCompile(DEVICE_INET_REGEXP)
	match := re.FindAllStringSubmatch(string(out), -1)
	var ip string
	for i := 0; i < len(match); i++ {
		ip = match[i][2]
		if ip != "127.0.0.1" {
			d.IP = ip
		}
	}
}

func (d *Device) connect() bool {
	ret := adbCmd("connect", d.IP_Port())
	if strings.Contains(string(ret), DEVICE_TCPIP_CONNECTED) {
		return true
	} else {
		return false
	}
}
func (d *Device) reconnect() bool {
	ret := d.disconnect()
	if ret != nil {
		return d.connect()
	}
	return false
}
func (d *Device) firstTimeConnectTcpIP() bool {
	d.openDeviceDefaultPort()
	d.getIP()
	return d.connect()
}

func (d *Device) UnlockPhone() {
	out := string(d.adbShellCmd(true, "dumpsys window policy"))
	if strings.Contains(out, DEVICE_ASLEEP) { // screen off
		d.adbShellCmd(true, "input keyevent POWER")
		out = string(d.adbShellCmd(true, "dumpsys window policy"))
	}
	if strings.Contains(out, DEVICE_AWAKE) {
		if strings.Contains(out, DEVICE_LOCKED) {
			//d.Password
			if d.Password == "" {
				d.adbShellCmd(true, "input text 000000")
			} else {
				d.adbShellCmd(true, fmt.Sprintf("input text %s", d.Password))
			}

			d.adbShellCmd(true, "input keyevent 66")
		} else if strings.Contains(out, DEVICE_UNLOCKED) {
			fmt.Println("unlocked")
		}
	}
}

func (s *ADBServer) getNonFreshedDevice(sn_or_ipPort string, refreshToken int64) (*Device, bool) { // found refreshed
	for id, d := range s.Devices {
		if d.SN == sn_or_ipPort || d.IP_Port() == sn_or_ipPort {
			if refreshToken != d.WireRefreshToken || refreshToken != d.WirelessRefreshToken {
				return &s.Devices[id], false
			}
			return &s.Devices[id], true
		}
	}
	return nil, false
}
func (s *ADBServer) GetDevice(sn_or_ipPort string) *Device {
	for _, d := range s.Devices {
		if d.SN == sn_or_ipPort || d.IP_Port() == sn_or_ipPort {
			return &d
		}
	}
	return nil
}
func (s *ADBServer) GetFirstAvailableDevice() *Device {
	for _, d := range s.Devices {
		if !d.Invalid && d.Idle {
			return &d
		}
	}
	return nil
}

func (s *ADBServer) RefreshDevice() {
	out := adbCmd("devices")
	timestamp := time.Now().Unix()
	deviceOutputs := strings.Split(strings.TrimSpace(string(out)), "\r")
	for i := len(deviceOutputs) - 1; deviceOutputs[i] != DEVICE_LIST_TITLE_LINE && i >= 0; i-- { // adb devices connected to the server
		deviceInfo := strings.Split(deviceOutputs[i], "\t") // first elem is sn or ipport, 2nd is the status: device or offline
		deviceId := strings.TrimSpace(deviceInfo[0])
		deviceStatus := strings.TrimSpace(deviceInfo[1])
		d, refreshed := s.getNonFreshedDevice(deviceId, timestamp)
		ip_port, isValidIP_Port := isIPPortValid(deviceId)
		if d != nil && !refreshed {
			if !isValidIP_Port {
				d.WireConnected = true
				d.WireRefreshToken = timestamp
			} else {
				if strings.Contains(deviceStatus, DEVICE_SUFFIX_CONNECTED) {
					d.WirelessConnected = true
				} else if strings.Contains(deviceStatus, DEVICE_SUFFIX_OFFLINE) {
					d.Invalid = true
					d.Idle = true
					connected := d.reconnect() // try reconnect
					if connected {
						d.Invalid = false
						d.WirelessConnected = true
					} else { // reconnect failed
						d.WirelessConnected = false
					}
				}
				d.WirelessRefreshToken = timestamp
			}
		} else if d == nil { // new device
			nd := newDevice()
			nd.Idle = true
			nd.Invalid = false
			if ip_port == "" {
				nd.SN = deviceId
				nd.WireConnected = true
				nd.firstTimeConnectTcpIP()
				nd.WirelessConnected = true
				nd.WirelessRefreshToken = timestamp
				nd.WireRefreshToken = timestamp
			} else {
				if strings.Contains(deviceStatus, DEVICE_SUFFIX_CONNECTED) {
					ip := strings.Split(ip_port, ":")[0]
					port := strings.Split(ip_port, ":")[1]
					nd.IP = ip
					nd.Port = port
					nd.getSerialNO() // in case ip changed
					nd.WirelessConnected = true
					nd.WirelessRefreshToken = timestamp
				} else if strings.Contains(deviceStatus, DEVICE_SUFFIX_OFFLINE) {
					connected := nd.reconnect() // try reconnect
					if connected {
						nd.getSerialNO()
						nd.WirelessConnected = true
					} else { // reconnect failed
						nd.WirelessConnected = false
					}
					nd.WirelessRefreshToken = timestamp
				}
			}
			s.Devices = append(s.Devices, *nd)
		}
	}
	for id, _ := range s.Devices {
		if s.Devices[id].WireRefreshToken < timestamp {
			s.Devices[id].WireConnected = false
		}
		if s.Devices[id].WirelessRefreshToken < timestamp {
			s.Devices[id].WirelessConnected = false
			if s.Devices[id].WireConnected && s.Devices[id].WireRefreshToken == timestamp {
				s.Devices[id].firstTimeConnectTcpIP()
				s.Devices[id].WirelessConnected = true
				s.Devices[id].WirelessRefreshToken = timestamp
			}
		}
		if !s.Devices[id].WireConnected && !s.Devices[id].WirelessConnected {
			s.Devices[id].Invalid = false
			s.Devices[id].Idle = false
		}
		// if s.Devices[id].Idle && !s.Devices[id].Invalid {
		// 	s.Devices[id].unlockPhone()
		// }
	}
}
