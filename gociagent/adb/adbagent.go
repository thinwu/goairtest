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
	DEVICE_INSTALL_APK_CMD = iota
	DEVICE_UNLOCK_PHONE
	DEVICE_RUN_TESTCASE
	DEVICE_INSTALL_APK_ARGS  = "-D%s"
	DEVICE_UNLOCK_PHONE_ARGS = "-D%s"
	DEVICE_CMD_RUN           = "runCMD -D%s"

	_IP_PORT_REGEXP                     = `(\d+\.\d+\.\d+\.\d+):\d+`
	_DEVICE_LIST_TITLE_LINE             = "List of devices attached"
	_DEVICE_SUFFIX_CONNECTED            = "device"
	_DEVICE_INET_REGEXP                 = `(inet\s)(\d+\.\d+\.\d+\.\d+)`
	_DEVICE_TCPIP_CONNECTED             = "connected to"
	_TCPIP_ALLREADY_CONNECTED           = "already connected to"
	_DEVICE_SUFFIX_OFFLINE              = "offline"
	_DEVICE_AWAKE                       = "interactiveState=INTERACTIVE_STATE_AWAKE"
	_DEVICE_ASLEEP                      = "interactiveState=INTERACTIVE_STATE_SLEEP"
	_DEVICE_LOCKED                      = "mInputRestricted=true"
	_DEVICE_UNLOCKED                    = "mInputRestricted=false"
	_DEVIVE_SELFCHECK_INTERVAL_SEC      = 10
	_SERVER_REFRESH_DEVIVE_INTERVAL_SEC = 30
	_DEVICE_LOCAL_IP                    = "127.0.0.1"
	_DEVICE_SHELL_SN                    = "getprop ro.boot.serialno"
	_DEVICE_STATUS_IDLE                 = iota
	_DEVICE_STATUS_RUNNING
)

type Message struct {
	Cmd            int
	Args           string
	Device         string
	UsingCableConn bool
}

func (m Message) tryGetArgsFromMsg() ([]string, int) {
	var args = make([]string, 0)
	for _, v := range strings.Split(m.Args, "-D") {
		if strings.TrimSpace(v) != "" {
			args = append(args, strings.TrimSpace(v))
		}
	}
	return args, len(args)
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
	DeviceStatus         int8
	Stop                 chan bool
	cmdChan              chan Message
	ticker               time.Ticker
	adbServer            *AdbServer
}

func newDevice(sn, ip, port string, s *AdbServer) *Device {
	d := &Device{SN: sn, IP: ip, Port: port,
		Password:     "000000",
		Stop:         make(chan bool, 1),
		DeviceStatus: _DEVICE_STATUS_IDLE,
		ticker:       *time.NewTicker(_DEVIVE_SELFCHECK_INTERVAL_SEC * time.Second),
		cmdChan:      make(chan Message, 5),
		adbServer:    s}
	d.goSelfCheck()
	d.goRunCmd()
	return d
}

type AdbServer struct {
	Devices       []*Device
	ticker        time.Ticker
	CmdChan       chan Message
	adbServerLock sync.RWMutex
}

func NewAdbServer() *AdbServer {
	s := &AdbServer{
		Devices: make([]*Device, 0),
		CmdChan: make(chan Message, 10),
		ticker:  *time.NewTicker(_SERVER_REFRESH_DEVIVE_INTERVAL_SEC * time.Second),
	}
	s.goRefreshDevice()
	s.goGetCmd()
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

func isIPPortValid(ip_port string) (string, bool) {
	re := regexp.MustCompile(_IP_PORT_REGEXP)
	ret := re.FindString(ip_port)
	if ret == "" {
		return "", false
	} else {
		return ret, true
	}
}

func (d *Device) get_IP_Port() string {
	return fmt.Sprintf("%s:%s", d.IP, d.Port)
}

func (d *Device) adbCmd(args ...string) []byte {
	if d.SN == "" {
		return nil
	}
	var cmd = append([]string{"-s", d.SN}, args...)
	return adbCmd(cmd...)
}
func (d *Device) stopApp(cablePriority bool, appName string) []byte {
	return d.adbShellCmd(cablePriority, "am", "force-stop", appName)
}

func (d *Device) startApp(cablePriority bool, appName string) []byte {
	return d.adbShellCmd(cablePriority, "monkey", "-p", appName, "1")
}

// cablePriority true: use sn connection first
func (d *Device) installAPK(cablePriority bool, apk string) []byte {
	d.DeviceStatus = DEVICE_INSTALL_APK_CMD
	device := d.SN
	if !cablePriority && d.IP != _DEVICE_LOCAL_IP {
		ip_port, valid := isIPPortValid(d.get_IP_Port())
		if valid {
			device = ip_port
			log.Printf("installing via tcpip: %v ", device)
		}
	} else {
		log.Printf("no public ip found, fallback to usb connection %v ", device)
	}
	defer func() {
		d.DeviceStatus = _DEVICE_STATUS_IDLE
		log.Printf("package %v installed ", apk)
	}()
	return adbCmd("-s", device, "install", "-r", apk)
}

// cablePriority true: using SN, false: try using tcpip, args ...string
func (d *Device) adbShellCmd(cablePriority bool, args ...string) []byte {
	device := d.SN
	if !cablePriority {
		ip_port, valid := isIPPortValid(d.get_IP_Port())
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
	return adbCmd("disconnect", d.get_IP_Port())
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
		re := regexp.MustCompile(_DEVICE_INET_REGEXP)
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

func (d *Device) runCmd(msg Message) {
	switch msg.Cmd {
	case DEVICE_INSTALL_APK_CMD:
		v, l := msg.tryGetArgsFromMsg()
		if l == 1 {
			d.installAPK(msg.UsingCableConn, v[0])
		}
	case DEVICE_UNLOCK_PHONE:
		d.unlockPhone()
	default:
		log.Printf("Device goRunCmd %v not found any suitable call", msg)
	}
}

func (d *Device) goRunCmd() {
	go func() {
		for {
			select {
			case <-d.Stop:
				log.Printf("device %v, %v Stop goRunCmd\n", d.SN, d.get_IP_Port())
				return
			case msg := <-d.cmdChan:
				log.Printf("device %v, %v readyToRunCmd\n", d.SN, d.get_IP_Port())
				d.runCmd(msg)
				// d.adbShellCmd(msg.UsingCableConn, msg.Cmd)
			}
		}
	}()
}

func (d *Device) goSelfCheck() {
	go func() {
		for {
			select {
			case <-d.Stop:
				close(d.cmdChan)
				close(d.Stop)
				d.ticker.Stop()
				d.adbServer.removeStoppedDevice(d)
				log.Printf("device %v, %v Stop goSelfCheck\n", d.SN, d.get_IP_Port())
				return
			case <-d.ticker.C:
				d.selfCheck()
			}
		}
	}()
}

func (d *Device) connect() bool {
	ret := adbCmd("connect", d.get_IP_Port())
	if strings.Contains(string(ret), _DEVICE_TCPIP_CONNECTED) {
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

func (d *Device) unlockPhone() {
	out := string(d.adbShellCmd(true, "dumpsys window policy"))
	if strings.Contains(out, _DEVICE_ASLEEP) { // screen off
		d.adbShellCmd(true, "input keyevent POWER")
		time.Sleep(200 * time.Millisecond) // waiting for the phone's screen on
		out = string(d.adbShellCmd(true, "dumpsys window policy"))
	}
	if strings.Contains(out, _DEVICE_AWAKE) {
		if strings.Contains(out, _DEVICE_LOCKED) {
			if d.Password == "" {
				d.adbShellCmd(true, "input text 000000")
			} else {
				d.adbShellCmd(true, fmt.Sprintf("input text %s", d.Password))
			}
		} else if strings.Contains(out, _DEVICE_UNLOCKED) {
			log.Printf("unlocked %s", d.SN)
		}
	}
}

func (s *AdbServer) removeStoppedDevice(stopped *Device) {
	s.adbServerLock.Lock()
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
	s.adbServerLock.Unlock()
}

func (s *AdbServer) getDevice(sn_or_ipPort string) *Device {
	s.adbServerLock.Lock()
	defer s.adbServerLock.Unlock()
	for _, d := range s.Devices {
		if d.SN == sn_or_ipPort || d.get_IP_Port() == sn_or_ipPort {
			return d
		}
	}
	return nil
}

func (s *AdbServer) printDeviceList() {
	s.adbServerLock.Lock()
	total := len(s.Devices)
	log.Printf("Current Device %v\n", total)
	for id, d := range s.Devices {
		log.Printf("\t#%v: sn: %v,tcpip: %v\n", (id + 1), d.SN, d.get_IP_Port())
	}
	s.adbServerLock.Unlock()
}

func (s *AdbServer) appendDevice(d *Device) {
	s.adbServerLock.Lock()
	defer s.adbServerLock.Unlock()
	s.Devices = append(s.Devices, d)
}

func (s *AdbServer) getAvailableDevice(sn_or_ipPort string) *Device {
	s.adbServerLock.Lock()
	defer s.adbServerLock.Unlock()
	for _, d := range s.Devices {
		if sn_or_ipPort != "" {
			if d.SN == sn_or_ipPort || d.get_IP_Port() == sn_or_ipPort {
				return d
			}
		} else if d.DeviceStatus == _DEVICE_STATUS_IDLE {
			return d
		}
	}
	return nil
}

func (s *AdbServer) goGetCmd() {
	go func() {
		for {
			msg := <-s.CmdChan
			log.Printf("AdbServer goGetCmd %v", msg)
			d := s.getAvailableDevice(msg.Device)
			if d != nil {
				d.cmdChan <- msg
			}
		}
	}()
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
				for i := len(deviceOutputs) - 1; deviceOutputs[i] != _DEVICE_LIST_TITLE_LINE && i >= 0; i-- { // adb devices connected to the server
					deviceInfo := strings.Split(deviceOutputs[i], "\t") // first elem is sn or ipport, 2nd is the status: device or offline
					deviceId := strings.TrimSpace(deviceInfo[0])
					if s.getDevice(deviceId) == nil {
						ip_port, valid := isIPPortValid(deviceId)
						if valid {
							ip := strings.Split(ip_port, ":")[0]
							port := strings.Split(ip_port, ":")[1]
							nd := newDevice("", ip, port, s)
							nd.getSerialNo(false)
							s.appendDevice(nd)
						} else {
							nd := newDevice(deviceId, "", "", s)
							s.appendDevice(nd)
						}
					}
				}
			}
		}
	}()
}
