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

var serverBaseURL = "http://localhost:9097" // server的地址

func main() {
	// 用 bufio.Scanner 包装标准输入，按行读取
	scanner := bufio.NewScanner(os.Stdin)

	printOptions()

	// 循环：只要还能读到下一行，就继续
	for scanner.Scan() {
		// scanner.Text() 拿到当前行（不含换行符）
		line := scanner.Text()

		if line == "1" {
			fmt.Println("当前所有待上传的文件列表(camID|sessionID|fileName):")
			var list []string = queryAllSessionsListNeedUpload(true)
			for _, item := range list {
				fmt.Println(item)
			}
			fmt.Println("共", len(list), "个录像")
		} else if line == "2" {
			fmt.Println("当前所有上传成功的文件列表(camID|sessionID|fileName):")
			var list []string = queryAllSessionsListNeedUpload(false)
			for _, item := range list {
				fmt.Println(item)
			}
			fmt.Println("共", len(list), "个录像")
		} else if line == "3" { // 获取minIO上已上传的文件列表
			fmt.Println("MinIO 上已有的文件列表:")
			listMinioObjects()
		} else if line == "4" { // 直接上传
			fmt.Println("输入要上传的 camID|sessionID|fileName :")
			scanner.Scan()
			line := scanner.Text()
			var parts []string = strings.Split(line, "|")
			if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
				fmt.Println("camID|sessionID|fileName格式错误")
				printOptions()
				continue
			}
			var camID string = parts[0]
			var sessionID string = parts[1]
			var fileName string = parts[2]
			fmt.Println("camID:", camID, ", sessionID:", sessionID, ", fileName:", fileName)
			// 检查文件夹是否存在
			var path string = filepath.Join("./EvtvcrForTest", camID, sessionID, fileName)
			if _, err := os.Stat(path); os.IsNotExist(err) {
				fmt.Println("文件夹不存在")
				printOptions()
				continue
			}
			fmt.Println("上传中...")
			if uploadFileViaServer(camID, sessionID, fileName, path) {
				fmt.Println("上传成功")
				addUploadedFlagToFileOrDir(path)
			} else {
				fmt.Println("上传失败")
			}
		} else if line == "5" { // 获取预签名url上传
			fmt.Println("输入要上传的 camID|sessionID|fileName :")
			scanner.Scan()
			line := scanner.Text()
			var parts []string = strings.Split(line, "|")
			if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
				fmt.Println("camID|sessionID|fileName格式错误")
				printOptions()
				continue
			}
			var camID string = parts[0]
			var sessionID string = parts[1]
			var fileName string = parts[2]
			fmt.Println("camID:", camID, ", sessionID:", sessionID, ", fileName:", fileName)
			// 检查文件夹是否存在
			var path string = filepath.Join("./EvtvcrForTest", camID, sessionID, fileName)
			if _, err := os.Stat(path); os.IsNotExist(err) {
				fmt.Println("文件夹不存在")
				printOptions()
				continue
			}
			presignedURL := getPresignedUrl(camID, sessionID, fileName, false)
			if presignedURL == "" {
				fmt.Println("获取预签名url失败")
				printOptions()
				continue
			}
			fmt.Println("预签名url(有效期10分钟):", presignedURL)
			fmt.Println("是否上传(y/n):")
			scanner.Scan()
			line = scanner.Text()
			if line == "y" {
				fmt.Println("上传中...")
				isSuccess := runPresignedURLForUpload(presignedURL, path)
				if isSuccess {
					fmt.Println("上传成功")
					addUploadedFlagToFileOrDir(path)
				} else {
					fmt.Println("上传失败")
				}
				printOptions()
				continue
			} else {
				fmt.Println("取消上传，可后续自行执行curl命令上传")
				printOptions()
				continue
			}
		} else if line == "6" { // 直接下载
			fmt.Println("输入要下载的 camID|sessionID|fileName :")
			scanner.Scan()
			line := scanner.Text()
			var parts []string = strings.Split(line, "|")
			if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
				fmt.Println("camID|sessionID|fileName格式错误")
				printOptions()
				continue
			}
			var camID string = parts[0]
			var sessionID string = parts[1]
			var fileName string = parts[2]
			fmt.Println("camID:", camID, ", sessionID:", sessionID, ", fileName:", fileName)
			fmt.Println("下载中...")
			ok, savedPath := downloadFileViaServer(camID, sessionID, fileName)
			if ok {
				fmt.Println("下载成功，文件已保存到:", savedPath)
			} else {
				fmt.Println("下载失败")
			}
		} else if line == "7" { // 获取预签名url下载
			fmt.Println("输入要下载的 camID|sessionID|fileName :")
			scanner.Scan()
			line := scanner.Text()
			var parts []string = strings.Split(line, "|")
			if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
				fmt.Println("camID|sessionID|fileName格式错误")
				printOptions()
				continue
			}
			var camID string = parts[0]
			var sessionID string = parts[1]
			var fileName string = parts[2]
			fmt.Println("camID:", camID, ", sessionID:", sessionID, ", fileName:", fileName)

			// 检查文件夹是否存在
			localDir := filepath.Join(".", "client", "PresignedURLDownloadFromMinIO")
			if err := os.MkdirAll(localDir, 0o755); err != nil {
				fmt.Println("创建本地下载目录失败:", err)
				printOptions()
				continue
			}
			presignedURL := getPresignedUrl(camID, sessionID, fileName, true)
			if presignedURL == "" {
				fmt.Println("获取预签名url失败")
				printOptions()
				continue
			}
			fmt.Println("预签名url(有效期10分钟):", presignedURL)
			fmt.Println("是否下载(y/n):")
			scanner.Scan()
			line = scanner.Text()
			if line == "y" {
				fmt.Println("下载中...")
				path := filepath.Join(localDir, filepath.Base(fileName))
				isSuccess := runPresignedURLForDownload(presignedURL, path)
				if isSuccess {
					fmt.Println("下载成功，文件已保存到:", path)
				} else {
					fmt.Println("下载失败")
				}
			} else {
				fmt.Println("取消下载，可后续自行执行curl命令下载")
			}
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
	fmt.Println("1. 查询当前所有待上传的文件列表")
	fmt.Println("2. 查询当前所有上传成功的文件列表")
	fmt.Println("3. 查询MinIO上已有的文件列表")
	fmt.Println("4. 输入要上传的session文件(camID|sessionID|fileName), 直接上传")
	fmt.Println("5. 输入要上传的session文件(camID|sessionID|fileName), 获取预签名url")
	fmt.Println("6. 输入要下载的session文件(camID|sessionID|fileName), 直接下载")
	fmt.Println("7. 输入要下载的session文件(camID|sessionID|fileName), 获取预签名url")
}

func queryAllSessionsListNeedUpload(isNeedUploaded bool) []string {
	// 目录结构：root/camID/sessionID/fileName
	// 返回格式：camID|sessionID|fileName
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

			entries, err := os.ReadDir(filepath.Join(root, camID, session.Name()))
			if err != nil {
				fmt.Println("出错:", err)
				continue
			}
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				if isNeedUploaded { // 待上传的
					if !strings.Contains(session.Name(), "uploaded") && !strings.Contains(entry.Name(), "uploaded") {
						list = append(list, camID+"|"+session.Name()+"|"+entry.Name())
					}
				} else { // 已上传的
					if strings.Contains(session.Name(), "uploaded") || strings.Contains(entry.Name(), "uploaded") {
						list = append(list, camID+"|"+session.Name()+"|"+entry.Name())
					}
				}
			}
		}
	}
	return list
}

// 向server申请上传或下载的预签名URL
func getPresignedUrl(camID, sessionID, fileName string, isDownload bool) string {
	q := url.Values{
		"camID":     {camID},
		"sessionID": {sessionID},
		"fileName":  {fileName},
	}
	if isDownload {
		q.Set("download", "true")
	}
	reqURL := serverBaseURL + "/presign?" + q.Encode()

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

func uploadFileViaServer(camID, sessionID, fileName, path string) bool {
	fi, err := os.Open(path)
	if err != nil {
		fmt.Println("打开文件失败:", err)
		return false
	}
	defer fi.Close()

	stat, err := fi.Stat()
	if err != nil {
		fmt.Println("读取文件信息失败:", err)
		return false
	}

	reqURL := serverBaseURL + "/upload?" + url.Values{
		"bucket": {"evtvcr"},
		"key":    {camID + "/" + sessionID + "/" + fileName},
	}.Encode()

	req, err := http.NewRequest(http.MethodPost, reqURL, fi)
	if err != nil {
		fmt.Println("创建上传请求失败:", err)
		return false
	}
	req.ContentLength = stat.Size()

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println("上传失败:", err)
		return false
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		fmt.Println("上传失败:", resp.Status, strings.TrimSpace(string(body)))
		return false
	}
	return true
}

func listMinioObjects() {
	reqURL := serverBaseURL + "/list?" + url.Values{
		"bucket": {"evtvcr"},
	}.Encode()

	resp, err := http.Get(reqURL)
	if err != nil {
		fmt.Println("请求文件列表失败:", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("读取文件列表失败:", err)
		return
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Println("请求文件列表失败:", resp.Status, strings.TrimSpace(string(body)))
		return
	}
	fmt.Print(string(body))
	if len(body) > 0 && body[len(body)-1] != '\n' {
		fmt.Println()
	}
}

// 执行预签名url上传文件
func runPresignedURLForUpload(presignedURL, path string) bool {
	fi, err := os.Open(path)
	if err != nil {
		fmt.Println("打开文件失败:", err)
		return false
	}
	defer fi.Close()

	stat, err := fi.Stat()
	if err != nil {
		fmt.Println("读取文件信息失败:", err)
		return false
	}

	req, err := http.NewRequest(http.MethodPut, presignedURL, fi)
	if err != nil {
		fmt.Println("创建上传请求失败:", err)
		return false
	}
	req.ContentLength = stat.Size()

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println("上传失败:", err)
		return false
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		fmt.Println("上传失败:", resp.Status, strings.TrimSpace(string(body)))
		return false
	}
	return true
}

// 执行预签名url下载文件
func runPresignedURLForDownload(presignedURL, path string) bool {
	if presignedURL == "" {
		fmt.Println("预签名url为空")
		return false
	}

	req, err := http.NewRequest(http.MethodGet, presignedURL, nil)
	if err != nil {
		fmt.Println("创建下载请求失败:", err)
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println("下载失败:", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Println("下载失败:", resp.Status, strings.TrimSpace(string(body)))
		return false
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Println("创建本地下载目录失败:", err)
		return false
	}
	f, err := os.Create(path)
	if err != nil {
		fmt.Println("创建本地文件失败:", err)
		return false
	}
	_, err = io.Copy(f, resp.Body)
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		if err == nil {
			err = closeErr
		}
		fmt.Println("写入本地文件失败:", err)
		_ = os.Remove(path)
		return false
	}
	return true
}

// 将此文件末尾添加 uploaded 标记，如果当前文件夹下全部文件都已上传，则将文件夹末尾添加 uploaded 标记
func addUploadedFlagToFileOrDir(path string) {
	const flag = ".uploaded"

	info, err := os.Stat(path)
	if err != nil {
		fmt.Println("添加uploaded标记失败:", err)
		return
	}

	if !info.IsDir() && !strings.Contains(filepath.Base(path), "uploaded") {
		newPath := path + flag
		if err := os.Rename(path, newPath); err != nil {
			fmt.Println("文件添加uploaded标记失败:", err)
			return
		}
		path = newPath
		info, err = os.Stat(path)
		if err != nil {
			fmt.Println("添加uploaded标记失败:", err)
			return
		}
	}

	dir := path
	if !info.IsDir() {
		dir = filepath.Dir(path)
	}
	if strings.Contains(filepath.Base(dir), "uploaded") {
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Println("检查文件夹uploaded标记失败:", err)
		return
	}
	fileCount := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		fileCount++
		if !strings.Contains(entry.Name(), "uploaded") {
			return
		}
	}
	if fileCount == 0 {
		return
	}
	if err := os.Rename(dir, dir+flag); err != nil {
		fmt.Println("文件夹添加uploaded标记失败:", err)
	}
}

// 下载文件到当前目录，返回是否成功以及本地保存路径。
func downloadFileViaServer(camID, sessionID, fileName string) (bool, string) {
	reqURL := serverBaseURL + "/download?" + url.Values{
		"bucket": {"evtvcr"},
		"key":    {camID + "/" + sessionID + "/" + fileName},
	}.Encode()

	resp, err := http.Get(reqURL)
	if err != nil {
		fmt.Println("下载失败:", err)
		return false, ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Println("下载失败:", resp.Status, strings.TrimSpace(string(body)))
		return false, ""
	}

	localDir := filepath.Join(".", "client", "DirectDownloadFromMinIO")
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		fmt.Println("创建本地下载目录失败:", err)
		return false, ""
	}
	localPath := filepath.Join(localDir, filepath.Base(fileName))
	f, err := os.Create(localPath)
	if err != nil {
		fmt.Println("创建本地文件失败:", err)
		return false, ""
	}

	_, err = io.Copy(f, resp.Body)
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		if err == nil {
			err = closeErr
		}
		fmt.Println("写入本地文件失败:", err)
		_ = os.Remove(localPath)
		return false, ""
	}
	return true, localPath
}
