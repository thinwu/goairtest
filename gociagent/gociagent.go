package main

import (
	"fmt"
	"gociagent/adb"
	"gociagent/poco"
	"log"
	"net"
	"net/http"
	"os"
)

var adbserver *adb.ADBServer
var testingTasks *poco.TestingTasks

func main() {
	DeviceMonitor()
	go http.HandleFunc("/reportself", reportself)
	go http.HandleFunc("/testRun", testRun)
	log.Fatal(http.ListenAndServe(":9988", nil))
}

func testRun(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" { // r.Method == "POST"
		// Get the POST data
		err := r.ParseForm()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		build := r.Form.Get("build")
		jobName := r.Form.Get("jobname")
		fmt.Fprintf(w, "build data: %s", build)
		fmt.Fprintf(w, "jobname data: %s", jobName)
		testingTasks = poco.LoadTaskRunConf()
		testingTasks.TryRunTestingTask(build, jobName, adbserver)
	} else {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
	}
}

func reportself(w http.ResponseWriter, r *http.Request) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				fmt.Println("IP Address:", ipnet.IP.String())
			}
		}
	}
	hostname, err := os.Hostname()
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("Hostname:", hostname)
	// noLoop()
	//http.HandleFunc("/testrun", testRun)
}

func DeviceMonitor() {
	fmt.Println("DeviceMonitor")
	adbserver = &adb.ADBServer{
		Devices: make([]*adb.Device, 0),
	}
	adbserver.GoRefreshDevice()
}
