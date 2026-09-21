package main

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var serverBaseURL = "http://localhost:9097"

func main() {
	// 用 bufio.Scanner 包装标准输入，按行读取
	scanner := bufio.NewScanner(os.Stdin)

	printOptions()

	// 循环：只要还能读到下一行，就继续
	for scanner.Scan() {
		// scanner.Text() 拿到当前行（不含换行符）
		line := scanner.Text()

		if line == "1" {
			fmt.Println("当前所有待上传的录像列表(camID-sessionID):")
			var list []string = queryAllSessionsListNeedUpload(true)
			for _, item := range list {
				fmt.Println(item)
			}
			fmt.Println("共", len(list), "个录像")
		} else if line == "2" {
			fmt.Println("当前所有上传成功的录像列表(camID-sessionID):")
			var list []string = queryAllSessionsListNeedUpload(false)
			for _, item := range list {
				fmt.Println(item)
			}
			fmt.Println("共", len(list), "个录像")
		} else if line == "3" { // 直接上传
			fmt.Println("请输入camID-sessionID:")
			scanner.Scan()
			line := scanner.Text()
			var parts []string = strings.Split(line, "-")
			if len(parts) != 2 {
				fmt.Println("camID-sessionID格式错误")
				printOptions()
				continue
			}
			var camID string = parts[0]
			var sessionID string = parts[1]
			fmt.Println("camID:", camID, ", sessionID:", sessionID)
			// 检查文件夹是否存在
			var path string = filepath.Join("./EvtvcrForTest", camID, sessionID)
			if _, err := os.Stat(path); os.IsNotExist(err) {
				fmt.Println("文件夹不存在")
				printOptions()
				continue
			}
			// 检查文件夹是否包含 uploaded 标记
			if strings.Contains(sessionID, "uploaded") {
				fmt.Println("文件夹已上传")
				printOptions()
				continue
			}
			// 直接调server接口上传文件到minIO
		} else if line == "4" { // 获取预签名url上传
			fmt.Println("请输入camID-sessionID:")
			scanner.Scan()
			line := scanner.Text()
			var parts []string = strings.Split(line, "-")
			if len(parts) != 2 {
				fmt.Println("camID-sessionID格式错误")
				printOptions()
				continue
			}
			var camID string = parts[0]
			var sessionID string = parts[1]
			fmt.Println("camID:", camID, ", sessionID:", sessionID)
			// 检查文件夹是否存在
			var path string = filepath.Join("./EvtvcrForTest", camID, sessionID)
			if _, err := os.Stat(path); os.IsNotExist(err) {
				fmt.Println("文件夹不存在")
				printOptions()
				continue
			}
			// 检查文件夹是否包含 uploaded 标记
			if strings.Contains(sessionID, "uploaded") {
				fmt.Println("文件夹已上传")
				printOptions()
				continue
			}
			presignedURL := getPresignedUrl(camID, sessionID)
			fmt.Println("预签名url:", presignedURL)
		} else if line == "5" { // 直接下载
			fmt.Println("请输入camID-sessionID:")
		} else if line == "6" { // 获取预签名url下载
			fmt.Println("请输入camID-sessionID:")
		}

		// 退出条件
		if line == "quit" || line == "q" {
			break
		}
		printOptions()
	}

	// 检查读取过程中是否出错（比如输入流中断）
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "读取错误:", err)
	}
}

func printOptions() {
	fmt.Printf("\n[%s] 输入内容（输入 quit or q 退出）：\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Println("1. 查询当前所有待上传的录像列表")
	fmt.Println("2. 查询当前所有上传成功的录像列表")
	fmt.Println("3. 输入要上传的session(camID-sessionID), 直接上传")
	fmt.Println("4. 输入要上传的session(camID-sessionID), 获取预签名url")
	fmt.Println("5. 输入要下载的session(camID-sessionID), 直接下载")
	fmt.Println("6. 输入要下载的session(camID-sessionID), 获取预签名url")
}

func queryAllSessionsListNeedUpload(isNeedUploaded bool) []string {
	// 目录结构：root/camID/sessionID/...
	// 返回格式：camID-sessionID
	root := "./EvtvcrForTest"
	list := make([]string, 0)

	camEntries, err := os.ReadDir(root)
	if err != nil {
		fmt.Println("出错:", err)
		return list
	}

	for _, cam := range camEntries {
		if !cam.IsDir() {
			continue
		}
		camID := cam.Name()
		sessionEntries, err := os.ReadDir(filepath.Join(root, camID))
		if err != nil {
			fmt.Println("出错:", err)
			continue
		}
		for _, session := range sessionEntries {
			if !session.IsDir() {
				continue
			}
			
			if isNeedUploaded {// 待上传的
				if strings.Contains(session.Name(), "uploaded") {
					continue
				}
			} else {// 只包含已上传的录像
				if !strings.Contains(session.Name(), "uploaded") {
					continue
				}
			}

			list = append(list, camID+"-"+session.Name())
		}
	}
	return list
}

// getPresignedUrl 向 server 申请上传用预签名 URL。
// 当前把整个 session 当作一个对象（key=camID/sessionID），不做文件切片。
func getPresignedUrl(camID, sessionID string) string {
	reqURL := serverBaseURL + "/presign?" + url.Values{
		"camID":     {camID},
		"sessionID": {sessionID},
	}.Encode()

	resp, err := http.Get(reqURL)
	if err != nil {
		fmt.Println("请求预签名url失败:", err)
		return ""
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("读取预签名url失败:", err)
		return ""
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Println("请求预签名url失败:", resp.Status, strings.TrimSpace(string(body)))
		return ""
	}
	return strings.TrimSpace(string(body))
}

