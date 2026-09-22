package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var serverBaseURL = "http://localhost:9097" // server的地址

const (
	multipartThreshold = 2 * 1024 * 1024 // 超过 2MB 走分片上传
	multipartPartSize  = 5 * 1024 * 1024 // S3/MinIO 非最后分片最小 5MB
)

type completedPartJSON struct {
	PartNumber int32  `json:"PartNumber"`
	ETag       string `json:"ETag"`
}

func main() {
	// 用 bufio.Scanner 包装标准输入，按行读取
	scanner := bufio.NewScanner(os.Stdin)

	printOptions()

	// 循环：只要还能读到下一行，就继续
	for scanner.Scan() {
		// scanner.Text() 拿到当前行（不含换行符）
		line := scanner.Text()
		fmt.Println("----------------------------------------------------------------");

		if line == "1" {
			fmt.Println("当前所有待上传的文件列表(camID|sessionID|fileName):")
			var list []string = queryAllSessionsListNeedUpload(true)
			for _, item := range list {
				fmt.Println(item)
			}
			fmt.Println("共", len(list), "个文件")
		} else if line == "2" {
			fmt.Println("当前所有上传成功的文件列表(camID|sessionID|fileName):")
			var list []string = queryAllSessionsListNeedUpload(false)
			for _, item := range list {
				fmt.Println(item)
			}
			fmt.Println("共", len(list), "个文件")
		} else if line == "3" { // 获取minIO上已上传的文件列表
			fmt.Println("MinIO 上已有的文件列表:")
			listMinioObjects()
		} else if line == "4" { // 删除MinIO上的指定文件
			fmt.Println("输入要删除的 camID|sessionID|fileName :")
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
			fmt.Println("删除中...")
			if deleteObjectFromMinio(camID, sessionID, fileName) {
				// 调整本地文件的uploaded标记，删除后，需要删除本地文件的uploaded标记，删除成功后，如果sessionID有uploaded后缀，也清除（因为sessionID的uploaded标记是整体session的）
				addOrRemoveUploadedFlagToFileOrDir(filepath.Join("./EvtvcrForTest", camID, sessionID, fileName), false)
			} else {
				fmt.Println("删除失败")
			}
		} else if line == "5" { // 直接上传，不考虑大文件切片上传
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
				addOrRemoveUploadedFlagToFileOrDir(path, true)
			} else {
				fmt.Println("上传失败")
			}
		} else if line == "6" { // 获取预签名url上传，考虑大文件切片上传
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
			info, err := os.Stat(path)
			if os.IsNotExist(err) {
				fmt.Println("文件夹不存在")
				printOptions()
				continue
			}
			isMultipartUpload := false
			if info.Size() > multipartThreshold {
				fmt.Println("文件大小 > 2MB，需分片上传")
				isMultipartUpload = true
			}
			var uploadID string
			var presignedURL string
			if isMultipartUpload {
				uploadID = initMultipartUpload(camID, sessionID, fileName)
				if uploadID == "" {
					fmt.Println("初始化分片上传失败")
					printOptions()
					continue
				}
				presignedURL = getPresignedPartUrl(camID, sessionID, fileName, uploadID, 1)
			} else {
				presignedURL = getPresignedUrl(camID, sessionID, fileName, false)
			}
			if presignedURL == "" {
				fmt.Println("获取预签名url失败")
				abortMultipartUpload(camID, sessionID, fileName, uploadID)
				printOptions()
				continue
			}
			if isMultipartUpload {
				fmt.Println("第一个分片的预签名url(有效期10分钟):", presignedURL)
			} else {
				fmt.Println("预签名url(有效期10分钟):", presignedURL)
			}
			fmt.Println("是否上传(y/n):")
			scanner.Scan()
			line = scanner.Text()
			if line == "y" {
				fmt.Println("上传中...")
				var isSuccess bool
				if isMultipartUpload {
					isSuccess = uploadMultipartFile(camID, sessionID, fileName, path, uploadID, presignedURL)
				} else {
					isSuccess = runPresignedURLForUpload(presignedURL, path)
				}
				if isSuccess {
					addOrRemoveUploadedFlagToFileOrDir(path, true)
				} else {
					fmt.Println("上传失败")
				}
			} else {
				abortMultipartUpload(camID, sessionID, fileName, uploadID)
			    fmt.Println("取消上传，可后续自行执行curl命令上传")
			}
		} else if line == "7" { // 直接下载
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
		} else if line == "8" { // 获取预签名url下载
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
	fmt.Println("\n1. 查询当前所有待上传的文件列表")
	fmt.Println("2. 查询当前所有上传成功的文件列表")
	fmt.Println("3. 查询MinIO上已有的文件列表")
	fmt.Println("4. 删除MinIO上的指定文件")
	fmt.Println("5. 输入要上传的session文件(camID|sessionID|fileName), 直接上传，大文件不考虑切片上传")
	fmt.Println("6. 输入要上传的session文件(camID|sessionID|fileName), 获取预签名url，大文件(>2MB)时考虑切片上传")
	fmt.Println("7. 输入要下载的session文件(camID|sessionID|fileName), 直接下载")
	fmt.Println("8. 输入要下载的session文件(camID|sessionID|fileName), 获取预签名url")
	fmt.Printf("[%s] 输入功能序号（输入 quit or q 退出）：", time.Now().Format("2006-01-02 15:04:05"))
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
	return requestPresignedUrl(camID, sessionID, fileName, isDownload, "", 0)
}

func getPresignedPartUrl(camID, sessionID, fileName, uploadID string, partNumber int) string {
	return requestPresignedUrl(camID, sessionID, fileName, false, uploadID, partNumber)
}

func requestPresignedUrl(camID, sessionID, fileName string, isDownload bool, uploadID string, partNumber int) string {
	q := url.Values{
		"camID":     {camID},
		"sessionID": {sessionID},
		"fileName":  {fileName},
	}
	if isDownload {
		q.Set("download", "true")
	}
	if uploadID != "" && partNumber > 0 {
		q.Set("uploadId", uploadID)
		q.Set("partNumber", strconv.Itoa(partNumber))
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
	if msg := strings.TrimSpace(string(body)); msg != "" {
		fmt.Println(msg)
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

func deleteObjectFromMinio(camID, sessionID, fileName string) bool {
	reqURL := serverBaseURL + "/delete?" + url.Values{
		"bucket": {"evtvcr"},
		"key":    {camID + "/" + sessionID + "/" + fileName},
	}.Encode()

	resp, err := http.Get(reqURL)
	if err != nil {
		fmt.Println("删除文件失败:", err)
		return false
	}
	defer resp.Body.Close()
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("读取删除文件失败:", err)
		return false
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Println("删除文件失败:", resp.Status, strings.TrimSpace(string(body)))
		return false
	}
	if msg := strings.TrimSpace(string(body)); msg != "" {
		fmt.Println(msg)
	}
	return true
}

func initMultipartUpload(camID, sessionID, fileName string) string {
	reqURL := serverBaseURL + "/multipart/init?" + url.Values{
		"camID":     {camID},
		"sessionID": {sessionID},
		"fileName":  {fileName},
	}.Encode()

	resp, err := http.Get(reqURL)
	if err != nil {
		fmt.Println("初始化分片上传失败:", err)
		return ""
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("读取分片上传 uploadId 失败:", err)
		return ""
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Println("初始化分片上传失败:", resp.Status, strings.TrimSpace(string(body)))
		return ""
	}
	return strings.TrimSpace(string(body))
}

func completeMultipartUpload(camID, sessionID, fileName, uploadID string, parts []completedPartJSON) bool {
	payload, err := json.Marshal(parts)
	if err != nil {
		fmt.Println("编码分片列表失败:", err)
		return false
	}
	reqURL := serverBaseURL + "/multipart/complete?" + url.Values{
		"camID":     {camID},
		"sessionID": {sessionID},
		"fileName":  {fileName},
		"uploadId":  {uploadID},
	}.Encode()

	req, err := http.NewRequest(http.MethodPost, reqURL, bytes.NewReader(payload))
	if err != nil {
		fmt.Println("创建合并分片请求失败:", err)
		return false
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println("合并分片失败:", err)
		return false
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		fmt.Println("合并分片失败:", resp.Status, strings.TrimSpace(string(body)))
		return false
	}
	if msg := strings.TrimSpace(string(body)); msg != "" {
		fmt.Println(msg)
	}
	return true
}

// 中止未完成的分片上传，删除 MinIO 上已写入的分片数据。
func abortMultipartUpload(camID, sessionID, fileName, uploadID string) {
	if uploadID == "" {
		return
	}
	reqURL := serverBaseURL + "/multipart/abort?" + url.Values{
		"camID":     {camID},
		"sessionID": {sessionID},
		"fileName":  {fileName},
		"uploadId":  {uploadID},
	}.Encode()

	resp, err := http.Get(reqURL)
	if err != nil {
		fmt.Println("删除未完成分片失败:", err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		fmt.Println("删除未完成分片失败:", resp.Status, strings.TrimSpace(string(body)))
		return
	}
	if msg := strings.TrimSpace(string(body)); msg != "" {
		fmt.Println(msg)
	}
}

// 执行预签名url上传文件,分片传输。任一分片失败时中止并删除已上传分片。
func uploadMultipartFile(camID, sessionID, fileName, path, uploadID, firstPresignedURL string) bool {
	fi, err := os.Open(path)
	if err != nil {
		fmt.Println("打开文件失败:", err)
		abortMultipartUpload(camID, sessionID, fileName, uploadID)
		return false
	}
	defer fi.Close()

	stat, err := fi.Stat()
	if err != nil {
		fmt.Println("读取文件信息失败:", err)
		abortMultipartUpload(camID, sessionID, fileName, uploadID)
		return false
	}

	totalSize := stat.Size()
	if totalSize == 0 {
		fmt.Println("空文件不支持分片上传")
		abortMultipartUpload(camID, sessionID, fileName, uploadID)
		return false
	}

	var parts []completedPartJSON
	var offset int64
	partNumber := 1
	presignedURL := firstPresignedURL

	for offset < totalSize {
		partSize := int64(multipartPartSize)
		remain := totalSize - offset
		if remain < partSize {
			partSize = remain
		}

		if presignedURL == "" {
			presignedURL = getPresignedPartUrl(camID, sessionID, fileName, uploadID, partNumber)
			if presignedURL == "" {
				fmt.Println("获取第", partNumber, "个分片的预签名url失败")
				abortMultipartUpload(camID, sessionID, fileName, uploadID)
				return false
			}
		}

		fmt.Printf("第%d个分片的预签名url(有效期10分钟): %s\n", partNumber, presignedURL)
		fmt.Printf("上传第%d个分片 (%d ~ %d 字节)...\n", partNumber, offset, offset+partSize-1)

		section := io.NewSectionReader(fi, offset, partSize)
		etag, ok := runPresignedURLForUploadPart(presignedURL, section, partSize)
		if !ok {
			abortMultipartUpload(camID, sessionID, fileName, uploadID)
			return false
		}
		parts = append(parts, completedPartJSON{
			PartNumber: int32(partNumber),
			ETag:       etag,
		})

		offset += partSize
		partNumber++
		presignedURL = ""
	}

	fmt.Println("分片上传完成，开始合并分片...")
	fmt.Println("分片合并参数为 uploadId: ", uploadID, ", ETags: ", parts)

	if !completeMultipartUpload(camID, sessionID, fileName, uploadID, parts) {
		abortMultipartUpload(camID, sessionID, fileName, uploadID)
		return false
	}
	return true
}

func runPresignedURLForUploadPart(presignedURL string, body io.Reader, size int64) (string, bool) {
	req, err := http.NewRequest(http.MethodPut, presignedURL, body)
	if err != nil {
		fmt.Println("创建分片上传请求失败:", err)
		return "", false
	}
	req.ContentLength = size

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println("分片上传失败:", err)
		return "", false
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		fmt.Println("分片上传失败:", resp.Status, strings.TrimSpace(string(respBody)))
		return "", false
	}
	etag := strings.TrimSpace(resp.Header.Get("ETag"))
	if etag == "" {
		fmt.Println("分片上传失败: 响应缺少 ETag")
		return "", false
	}
	fmt.Println("分片上传成功, ETag:", etag)
	return etag, true
}

// 执行预签名url上传文件,不分片传输
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
	if msg := strings.TrimSpace(string(body)); msg != "" {
		fmt.Println(msg)
	} else {
		u, err := url.Parse(presignedURL)
		if err != nil {
			fmt.Println("上传成功: ", presignedURL)
		} else {
			remotePath := strings.TrimPrefix(u.Path, "/")
			fmt.Println("上传成功: ", remotePath)
		}
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

// isAdd=true：给文件加上 .uploaded；该目录下文件都已标记时，再给 session 目录加上 .uploaded。
// isAdd=false：去掉文件的 .uploaded；若 session 目录带了 .uploaded，一并去掉（目录标记表示整个 session 已上传）。
func addOrRemoveUploadedFlagToFileOrDir(path string, isAdd bool) {
	const flag = ".uploaded"

	if !isAdd {
		actual, err := resolveLocalUploadPath(path, flag)
		if err != nil {
			fmt.Println("清除uploaded标记失败:", err)
			return
		}
		if strings.HasSuffix(filepath.Base(actual), flag) {
			newPath := strings.TrimSuffix(actual, flag)
			if err := os.Rename(actual, newPath); err != nil {
				fmt.Println("文件清除uploaded标记失败:", err)
				return
			}
			actual = newPath
		}
		dir := filepath.Dir(actual)
		if strings.HasSuffix(filepath.Base(dir), flag) {
			if err := os.Rename(dir, strings.TrimSuffix(dir, flag)); err != nil {
				fmt.Println("文件夹清除uploaded标记失败:", err)
			}
		}
		return
	}

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

func resolveLocalUploadPath(path, flag string) (string, error) {
	candidates := []string{path, path + flag}
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	altDir := dir + flag
	candidates = append(candidates,
		filepath.Join(altDir, base),
		filepath.Join(altDir, base+flag),
	)
	if strings.HasSuffix(base, flag) {
		plain := strings.TrimSuffix(base, flag)
		candidates = append(candidates,
			filepath.Join(dir, plain),
			filepath.Join(altDir, plain),
		)
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("本地文件不存在: %s", path)
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
