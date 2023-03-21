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
	_DEVICE_LOCAL_IP                    = "127.0.0.1"
	_DEVICE_SHELL_SN                    = "getprop ro.boot.serialno"
	_DEVICE_STATUS_IDLE                 = iota
	_DEVICE_STATUS_RUNNING
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
	DeviceStatus         int16
	CmdChan              chan Message
	Stop                 chan bool
	ticker               time.Ticker
	AdbServer            *AdbServer
}

func newDevice(sn, ip, port string, s *AdbServer) *Device {
	d := &Device{SN: sn, IP: ip, Port: port,
		CmdChan: make(chan Message, 5), Stop: make(chan bool, 1),
		DeviceStatus: _DEVICE_STATUS_IDLE,
		ticker:       *time.NewTicker(_DEVIVE_SELFCHECK_INTERVAL_SEC * time.Second),
		AdbServer:    s}
	d.goSelfCheck()
	d.goRunCmd()
	return d
}

type AdbServer struct {
	Devices    []*Device
	ticker     time.Ticker
	deviceLock sync.RWMutex
}

func NewAdbServer() *AdbServer {
	s := &AdbServer{
		Devices: make([]*Device, 0),
		ticker:  *time.NewTicker(_SERVER_REFRESH_DEVIVE_INTERVAL_SEC * time.Second),
	}
	s.goRefreshDevice()
	return s
}

func adbCmd(args ...string) []byte {
	cmd := exec.Command("adb", args...)
	log.Printf("adbCmd: %v \n", cmd)
	out, err := cmd.Output()
	if err != nil {
		log.Printf("\terr adbCmd: %v \n\t%v\n", cmd, err)
		return nil
	}
	return out
}

func (d *Device) goRunCmd() {
	go func() {
		for {
			select {
			case <-d.Stop:
				log.Printf("device %v, %v Stop goRunCmd\n", d.SN, d.IP_Port())
				return
			default:
				log.Printf("device %v, %v readyToRunCmd\n", d.SN, d.IP_Port())
				msg := <-d.CmdChan
				d.adbShellCmd(msg.UsingCableConn, msg.Cmd)
			}
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
	var b = d.adbCmd("tcpip", port)
	d.Port = port
	time.Sleep(2000 * time.Millisecond) // waiting for the phone restarting in tcp mode
	return b
}
func (d *Device) disconnect() []byte {
	return adbCmd("disconnect", d.IP_Port())
}
func (d *Device) getSerialNo(cablePriority bool) bool {
	if cablePriority && d.SN == "" {
		return false
	} else if !cablePriority && d.IP == "" {
		return false
	}
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
			d.IP = _DEVICE_LOCAL_IP
			if ip != _DEVICE_LOCAL_IP {
				d.IP = ip
				return true
			}
		}
	}
	return false
}
func (d *Device) selfCheck() {
	log.Printf("Device %v selfCheck\n", d.SN)
	timestamp := time.Now().Unix()
	if !d.getSerialNo(true) {
		d.WireConnected = false
	} else {
		d.WireConnected = true
	}
	d.WireRefreshToken = timestamp

	if d.IP != _DEVICE_LOCAL_IP {
		if !d.getSerialNo(false) {
			if d.IP == "" {
				if !d.initTcpIPConn() {
					d.WirelessConnected = false
				} else {
					d.WirelessConnected = true
				}
			} else {
				if d.reconnect() {
					d.WirelessConnected = true
				} else {
					d.WirelessConnected = false
				}
			}

		} else {
			d.WirelessConnected = true
		}
	} else {
		d.WirelessConnected = false
	}
	d.WirelessRefreshToken = timestamp
	if !d.WireConnected && !d.WirelessConnected {
		log.Printf("Device %v selfCheck is invalid\n", d.SN)
		d.Stop <- true
	}
}
func (d *Device) goSelfCheck() {
	go func() {
		for {
			select {
			case <-d.Stop:
				close(d.CmdChan)
				close(d.Stop)
				d.ticker.Stop()
				d.AdbServer.removeStoppedDevice(d)
				log.Printf("device %v, %v Stop goSelfCheck\n", d.SN, d.IP_Port())
				return
			case <-d.ticker.C:
				d.selfCheck()
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
func (d *Device) initTcpIPConn() bool {
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
			log.Printf("unlocked %s", d.SN)
		}
	}
}

func (s *AdbServer) removeStoppedDevice(stopped *Device) {
	s.deviceLock.Lock()
	i := 0
	for _, d := range s.Devices {
		if d != stopped {
			s.Devices[i] = d
			i++
		}
	}
	for j := i; j < len(s.Devices); j++ {
		s.Devices[j] = nil
	}

	s.Devices = s.Devices[:i]
	log.Printf("device removed, current connecting device count %v", len(s.Devices))
	s.deviceLock.Unlock()
}

func (s *AdbServer) GetDevice(sn_or_ipPort string) *Device {
	s.deviceLock.Lock()
	defer s.deviceLock.Unlock()
	for _, d := range s.Devices {
		if d.SN == sn_or_ipPort || d.IP_Port() == sn_or_ipPort {
			return d
		}
	}

	return nil
}

func (s *AdbServer) printDeviceList() {
	s.deviceLock.Lock()
	total := len(s.Devices)
	log.Printf("Current Device %v\n", total)
	for id, d := range s.Devices {
		log.Printf("\t#%v: sn: %v,tcpip: %v\n", (id + 1), d.SN, d.IP_Port())
	}
	s.deviceLock.Unlock()
}

func (s *AdbServer) AppendDevice(d *Device) {
	s.deviceLock.Lock()
	defer s.deviceLock.Unlock()
	s.Devices = append(s.Devices, d)
}
func (s *AdbServer) GetFirstAvailableDevice() *Device {
	s.deviceLock.Lock()
	defer s.deviceLock.Unlock()
	for _, d := range s.Devices {
		if d.DeviceStatus == _DEVICE_STATUS_IDLE {
			return d
		}
	}
	return nil
}

func (s *AdbServer) goRefreshDevice() {
	go func() {
		for {
			select {
			case <-time.After(_DEVIVE_SELFCHECK_INTERVAL_SEC * time.Second):
				s.printDeviceList()
			case <-s.ticker.C:
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
							nd := newDevice("", ip, port, s)
							nd.getSerialNo(false)
							s.AppendDevice(nd)
						} else {
							nd := newDevice(deviceId, "", "", s)
							s.AppendDevice(nd)
						}
					}
				}
			}
		}
	}()
}
