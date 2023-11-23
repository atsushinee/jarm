package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/httputil"
	"os"
	"strconv"
	"time"
)

const chunkSize = 5 * 1024 * 1024

var client = &http.Client{
	Timeout: 0,
}

func main() {
	deployments := list()
	var projects []Deployment
	var runningProjects []Deployment
	var stoppedProjects []Deployment
	initProjects(deployments, &projects, &runningProjects, &stoppedProjects)

	if projects == nil {
		log.Fatal("project not found.")
		return
	}

	var deployment Deployment

	if len(projects) == 1 {
		deployment = projects[0]

		if len(runningProjects) == 1 {
			if config.ForceStop {
				fmt.Println(deployment.ProjectName, deployment.ServiceName, deployment.DepolyPort, "正在运行，停止中...")
				stop(deployment.ProjectID)

				runningProjects = nil
				stoppedProjects = append(stoppedProjects, deployment)
			} else {
				fmt.Println("project is running.")
				return
			}
		}
	}

	printStoppedProjects(stoppedProjects)
	printRunningProjects(runningProjects)

	if stoppedProjects == nil {
		log.Fatal("no stopped projects.")
		return
	}
	deployment = stoppedProjects[0]

	startDeployment(deployment)

	signal := make(chan struct{})
	waitRealStart(deployment, signal)
	<-signal
	fmt.Println(deployment.ProjectName, deployment.ServiceName, deployment.DepolyPort, "已启动")

	time.Sleep(1 * time.Second)
	if len(runningProjects) > 0 {
		fmt.Println("停止其他项目...")
		stopRunning(runningProjects)
	}
}

func initProjects(deployments []Deployment, projects, runningProjects, stoppedProjects *[]Deployment) {
	for _, deployment := range deployments {
		for _, projectId := range config.ProjectIdList {
			if deployment.ProjectID == projectId {
				*projects = append(*projects, deployment)
				if deployment.ProjectStatus == "运行中" {
					*runningProjects = append(*runningProjects, deployment)
				} else {
					*stoppedProjects = append(*stoppedProjects, deployment)
				}
			}
		}
	}

}

func printStoppedProjects(projects []Deployment) {
	for _, project := range projects {
		fmt.Println("未启动项目:", project.ProjectName, project.ServiceName, project.DepolyPort)
	}
}

func printRunningProjects(projects []Deployment) {
	for _, project := range projects {
		fmt.Println("运行中项目:", project.ProjectName, project.ServiceName, project.DepolyPort)
	}
}

func stopRunning(runningProjects []Deployment) {
	for _, project := range runningProjects {
		time.Sleep(1 * time.Second)
		stop(project.ProjectID)
		fmt.Println(project.ProjectName, project.ServiceName, project.DepolyPort, "已停止")
	}
}

func waitRealStart(deployment Deployment, signal chan struct{}) {
	go func() {
		realStart := false
		for !realStart {
			err := test("http://" + config.Host + ":" + deployment.DepolyPort)
			if err != nil {
				fmt.Printf("\r等待启动完成...")
				time.Sleep(5 * time.Second)
			} else {
				realStart = true
			}
		}
		fmt.Println()
		signal <- struct{}{}
	}()
}

func startDeployment(deployment Deployment) {
	fmt.Println(deployment.ProjectName, deployment.ServiceName, deployment.DepolyPort)
	fmt.Println("卸载...")
	uninstall(deployment.ProjectID)
	time.Sleep(1 * time.Second)

	fmt.Println("上传...")
	upload(config.JarPath, deployment.ProjectID)
	time.Sleep(1 * time.Second)
	fmt.Println("安装...")
	install(deployment.ProjectID)

	time.Sleep(1 * time.Second)
	fmt.Println("启动...")
	start(deployment.ProjectID)
	fmt.Println(deployment.ProjectName, deployment.ServiceName, deployment.DepolyPort, "正在启动...")
}

func debug(v ...any) {
	if config.Debug {
		log.Println(v)
	}
}

func list() []Deployment {
	resp, err := request("/project/list", Param{})
	if err != nil {
		panic(err)
	}
	response := Response{}
	json.Unmarshal([]byte(resp), &response)
	return response.Data.Obj
}

func stop(projectId string) {
	_, err := request("/depoly/stop", Param{ProjectId: projectId})
	if err != nil {
		log.Println("stop err", err)
		panic(err)
	}
}

func start(projectId string) {
	_, err := request("/depoly/start", Param{ProjectId: projectId})
	if err != nil {
		log.Println("start err", err)
		panic(err)
	}
}

func uninstall(projectId string) {
	_, err := request("/depoly/uninstall", Param{ProjectId: projectId})
	if err != nil {
		log.Println("uninstall err", err)
		panic(err)
	}
}

func install(projectId string) {
	_, err := request("/depoly/install", Param{ProjectId: projectId})
	if err != nil {
		log.Println("install err", err)
		panic(err)
	}
}

func upload(filepath string, projectId string) {
	uploadFile("/depoly/upload", filepath, projectId)
}

func uploadFile(route string, filepath string, projectId string) {
	file, err := os.Open(filepath)
	if err != nil {
		panic(err)
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		panic(err)
	}
	fileSize := stat.Size()
	f := float64(fileSize) / float64(chunkSize)

	chunkCount := int(math.Ceil(f))
	fmt.Println("上传文件 jar", filepath)
	fmt.Println("文件大小 filesize", fileSize)
	fmt.Println("分片大小 chunkSize", chunkSize)
	fmt.Println("分片数量 chunkCount", chunkCount)
	for i := 0; i < chunkCount; i++ {
		err := uploadChunk(route, projectId, chunkCount, i, file)
		if err != nil {
			panic(err)
		}
		fmt.Printf("\r上传进度: %.2f %s", math.Round((float64(i+1)/float64(chunkCount))*10000)/100, "%")
	}
	fmt.Println()
}

func uploadChunk(route, projectId string, chunkCount int, i int, file *os.File) error {
	buffer := &bytes.Buffer{}
	multipartWriter := multipart.NewWriter(buffer)

	multipartWriter.WriteField("chunkCount", strconv.Itoa(chunkCount))
	multipartWriter.WriteField("chunkIndex", strconv.Itoa(i))
	multipartWriter.WriteField("projectID", projectId)

	var buf = make([]byte, chunkSize)
	n, err := file.Read(buf)

	if err != nil {
		if err == io.EOF {
			return nil
		}
		return err
	}
	headers := map[string]string{
		"Content-Type":  multipartWriter.FormDataContentType(),
		"Authorization": config.Authorization,
	}
	part, err := multipartWriter.CreateFormFile("file", file.Name())

	_, err = io.CopyN(part, bytes.NewReader(buf), int64(n))
	if err != nil {
		return err
	}
	err = multipartWriter.Close()
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", config.BaseUrl+route, buffer)
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	resBody, _ := io.ReadAll(resp.Body)
	reqDump, _ := httputil.DumpRequest(req, false)
	debug(string(reqDump))
	if resp.StatusCode == http.StatusOK {
		debug(resp.Status, string(resBody))
		return nil

	} else {
		err := errors.New("upload error: " + resp.Status)
		return err
	}
}

func request(route string, param Param) (string, error) {
	paramString := param.toString()
	debug(paramString)
	marshal, _ := json.Marshal(param)
	reader := bytes.NewReader(marshal)

	req, err := http.NewRequest("POST", config.BaseUrl+route, reader)
	if err != nil {
		panic(err)
	}

	req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	req.Header.Set("Authorization", config.Authorization)

	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	resBody, _ := io.ReadAll(resp.Body)
	reqDump, _ := httputil.DumpRequest(req, false)
	debug(string(reqDump))
	if resp.StatusCode == http.StatusOK {
		debug(resp.Status, string(resBody))
		return string(resBody), nil

	}

	return "", errors.New("Request error: " + route + " : " + resp.Status)
}

func test(url string) error {
	req, err := http.NewRequest("GET", url, nil)
	_, err = client.Do(req)
	return err
}

type Param struct {
	ProjectId string `json:"projectID"`
}

func (p Param) toString() string {
	marshal, _ := json.Marshal(p)
	return string(marshal)
}

type Deployment struct {
	CreateDate        string `json:"createDate"`
	DependService     string `json:"dependService"`
	DepolyAfterParam  string `json:"depolyAfterParam"`
	DepolyBeforeParam string `json:"depolyBeforeParam"`
	DepolyPort        string `json:"depolyPort"`
	DepolyRemark      string `json:"depolyRemark"`
	JarPackagePath    string `json:"jarPackagePath"`
	LastDepolyDate    string `json:"lastDepolyDate"`
	ProjectID         string `json:"projectID"`
	ProjectName       string `json:"projectName"`
	ProjectStatus     string `json:"projectStatus"`
	ServiceName       string `json:"serviceName"`
}

type Data struct {
	Obj []Deployment `json:"obj"`
}

type Response struct {
	Code int    `json:"code"`
	Data Data   `json:"data"`
	Msg  string `json:"msg"`
}
