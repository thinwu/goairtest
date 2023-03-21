package poco

import (
	"encoding/json"
	"fmt"
	"gociagent/adb"
	"io/ioutil"
	"log"
	"os"
)

const (
	AIRTEST_EXE  = "AirtestIDE.exe"
	PKG_FOLDER_1 = ""
)

type SingleRun struct {
	TestCase        string `json:"testcase"`
	Device          string `json:"device"`
	DeviceUnlockPwd string `json:"deviceunlockpwd"`
	JobName         string `json:"jobname"`
	Build           string
}

type TestingTasks struct {
	AirTestEXE string      `json:"airtestexe"`
	TaskRun    []SingleRun `json:"taskrun"`
}

func (sr *SingleRun) runTest(s *adb.AdbServer) string { // return report file path
	log.Printf("%v", adb.DEVICE_INSTALL_APK_ARGS)
	log.Printf("%v", adb.DEVICE_INSTALL_APK_CMD)
	s.CmdChan <- adb.Message{
		Cmd:            adb.DEVICE_INSTALL_APK_CMD,
		Args:           fmt.Sprintf(adb.DEVICE_INSTALL_APK_ARGS, sr.Build),
		Device:         sr.Device,
		UsingCableConn: false,
	}
	s.CmdChan <- adb.Message{
		Cmd:            adb.DEVICE_UNLOCK_PHONE,
		Args:           fmt.Sprintf(adb.DEVICE_INSTALL_APK_ARGS, sr.DeviceUnlockPwd),
		Device:         sr.Device,
		UsingCableConn: false,
	}
	return ""

}

func (tt *TestingTasks) getSingleRunByJobName(jobName string) []SingleRun {
	srList := make([]SingleRun, 0)
	for _, sr := range tt.TaskRun {
		if sr.JobName == jobName {
			srList = append(srList, sr)
		}
	}
	return srList
}

func (tt *TestingTasks) TryRunTestingTask(build, jobName string, adbserver *adb.AdbServer) {
	var srList = tt.getSingleRunByJobName(jobName)
	for _, sr := range srList {
		sr.Build = build
		sr.runTest(adbserver)
	}
}

func LoadTaskRunConf() *TestingTasks {
	fileContent, err := os.Open("./poco/taskrun.json")

	if err != nil {
		log.Fatal(err)
		return nil
	}

	fmt.Println("The File is opened successfully...")

	defer fileContent.Close()

	byteResult, _ := ioutil.ReadAll(fileContent)

	var tasks TestingTasks

	err = json.Unmarshal(byteResult, &tasks)
	if err != nil {
		fmt.Printf("json.Unmarshal:taskrun.json error\n%v ", err)
	}
	fmt.Println("AirTestEXE: " + tasks.AirTestEXE)
	for i := 0; i < len(tasks.TaskRun); i++ {
		fmt.Println("TestCase: " + tasks.TaskRun[i].TestCase)
		fmt.Println("Device: " + tasks.TaskRun[i].Device)
		fmt.Println("JobName: " + tasks.TaskRun[i].JobName)
		fmt.Println("Build: " + tasks.TaskRun[i].Build)
	}
	return &tasks
}
