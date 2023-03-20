package adb

import (
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	IP_PORT_REGEXP                      = `(\d+\.\d+\.\d+\.\d+):\d+`
	DEVICE_LIST_TITLE_LINE              = "List of devices attached"
	DEVICE_SUFFIX_CONNECTED             = "device"
	DEVICE_INET_REGEXP                  = `(inet\s)(\d+\.\d+\.\d+\.\d+)`
	DEVICE_TCPIP_CONNECTED              = "connected to"
	TCPIP_ALLREADY_CONNECTED            = "already connected to"
	DEVICE_SUFFIX_OFFLINE               = "offline"
	DEVICE_AWAKE                        = "interactiveState=INTERACTIVE_STATE_AWAKE"
	DEVICE_ASLEEP                       = "interactiveState=INTERACTIVE_STATE_SLEEP"
	DEVICE_LOCKED                       = "mInputRestricted=true"
	DEVICE_UNLOCKED                     = "mInputRestricted=false"
	_DEVIVE_SELFCHECK_INTERVAL_SEC      = 10
	_SERVER_REFRESH_DEVIVE_INTERVAL_SEC = 30
	_DEVICE_SHELL_SN                    = "getprop ro.boot.serialno"
)

type Message struct {
	Cmd            string
	UsingCableConn bool
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
	Invalid              chan bool
	Idle                 bool
	C                    chan Message
	ADBServer            *ADBServer
}
type ADBServer struct {
	Devices    []*Device
	deviceLock sync.RWMutex
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

func (d *Device) goRunCmd() {
	go func() {
		for {
			fmt.Printf("device %v listenToCmdAndRun", d.SN)
			msg := <-d.C
			d.adbShellCmd(msg.UsingCableConn, msg.Cmd)
		}
	}()
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
func newDevice(sn, ip, port string) *Device {
	d := &Device{SN: sn, IP: ip, Port: port}
	d.goSelfCheck()
	d.goRunCmd()
	return d
}

func (d *Device) adbCmd(args ...string) []byte {
	if d.SN == "" {
		return nil
	}
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

func (d *Device) openDeviceDefaultPort() []byte {
	port := "5555"
	return d.openDevicePort(port)
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
func (d *Device) getSerialNo(cablePriority bool) bool {
	out := d.adbShellCmd(cablePriority, "getprop", "ro.boot.serialno")
	if out == nil {
		return false
	} else {
		d.SN = strings.TrimSpace(string(out))
		return true
	}
}
func (d *Device) getIP() bool {
	out := d.adbShellCmd(true, "ip", "-f", "inet", "addr", "show")
	if out != nil {
		re := regexp.MustCompile(DEVICE_INET_REGEXP)
		match := re.FindAllStringSubmatch(string(out), -1)
		var ip string
		for i := 0; i < len(match); i++ {
			ip = match[i][2]
			if ip != "127.0.0.1" {
				d.IP = ip
				return true
			}
		}
	}
	return false
}
func (d *Device) selfCheck() {
	timestamp := time.Now().Unix()
	if d.getSerialNo(true) == false {
		d.WireConnected = false
	} else {
		d.WireConnected = true
	}
	d.WireRefreshToken = timestamp

	if d.getSerialNo(false) == false {
		if !d.firstTimeConnectTcpIP() {
			d.WirelessConnected = false
		} else {
			d.WirelessConnected = true
		}
	} else {
		d.WirelessConnected = true
	}
	d.WirelessRefreshToken = timestamp
	if !d.WireConnected && !d.WirelessConnected {
		d.Invalid <- false
	}
}
func (d *Device) goSelfCheck() {
	ticker := time.NewTicker(_DEVIVE_SELFCHECK_INTERVAL_SEC * time.Second)
	go func() {
		for {
			select {
			case <-d.Invalid:
				fmt.Printf("device %v is Invalid", d.SN)
				d.ADBServer.removeDevice(d)
				return
			case <-ticker.C:
				d.selfCheck()
				return
			}
		}
	}()

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
	ret := d.openDeviceDefaultPort()
	if ret != nil {
		if d.getIP() {
			return d.connect()
		}
	}
	return false
}

func (d *Device) UnlockPhone() {
	out := string(d.adbShellCmd(true, "dumpsys window policy"))
	if strings.Contains(out, DEVICE_ASLEEP) { // screen off
		d.adbShellCmd(true, "input keyevent POWER")
		time.Sleep(200 * time.Millisecond) // waiting for the phone's screen on
		out = string(d.adbShellCmd(true, "dumpsys window policy"))
	}
	if strings.Contains(out, DEVICE_AWAKE) {
		if strings.Contains(out, DEVICE_LOCKED) {
			if d.Password == "" {
				d.adbShellCmd(true, "input text 000000")
			} else {
				d.adbShellCmd(true, fmt.Sprintf("input text %s", d.Password))
			}
		} else if strings.Contains(out, DEVICE_UNLOCKED) {
			fmt.Printf("unlocked %s", d.SN)
		}
	}
}

func (s *ADBServer) removeDevice(invalidDevice *Device) {
	s.deviceLock.Lock()
	i := 0
	for _, d := range s.Devices {
		if d != invalidDevice {
			s.Devices[i] = d
			i++
		}
	}
	for j := i; j < len(s.Devices); j++ {
		s.Devices[j] = nil
	}
	s.Devices = s.Devices[:i]
	s.deviceLock.Unlock()
}

func (s *ADBServer) GetDevice(sn_or_ipPort string) *Device {
	s.deviceLock.RLock()
	for _, d := range s.Devices {
		if d.SN == sn_or_ipPort || d.IP_Port() == sn_or_ipPort {
			return d
		}
	}
	s.deviceLock.RUnlock()
	return nil
}

func (s *ADBServer) AppendDevice(d *Device) {
	s.deviceLock.Lock()
	s.Devices = append(s.Devices, d)
	s.deviceLock.Unlock()
}
func (s *ADBServer) GetFirstAvailableDevice() *Device {
	for _, d := range s.Devices {
		if d.Idle {
			return d
		}
	}
	return nil
}

func (s *ADBServer) GoRefreshDevice() {
	ticker := time.NewTicker(_SERVER_REFRESH_DEVIVE_INTERVAL_SEC * time.Second)
	go func() {
		for {
			select {
			case <-ticker.C:
				out := adbCmd("devices")
				deviceOutputs := strings.Split(strings.TrimSpace(string(out)), "\r")
				for i := len(deviceOutputs) - 1; deviceOutputs[i] != DEVICE_LIST_TITLE_LINE && i >= 0; i-- { // adb devices connected to the server
					deviceInfo := strings.Split(deviceOutputs[i], "\t") // first elem is sn or ipport, 2nd is the status: device or offline
					deviceId := strings.TrimSpace(deviceInfo[0])
					if s.GetDevice(deviceId) == nil {
						ip_port, valid := isIPPortValid(deviceId)
						if valid {
							ip := strings.Split(ip_port, ":")[0]
							port := strings.Split(ip_port, ":")[1]
							nd := newDevice("", ip, port)
							s.AppendDevice(nd)
						} else {
							nd := newDevice(deviceId, "", "")
							s.AppendDevice(nd)
						}
					}
				}
				return
			}
		}
	}()
}
