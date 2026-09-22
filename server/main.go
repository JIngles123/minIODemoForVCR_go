package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// ---------- 配置 ----------
const (
	MinioEndpoint  = "http://localhost:9000" // 你的 MinIO 地址
	MinioAccessKey = "jlmtest"               // 默认用户名
	MinioSecretKey = "jlmtestpwd"            // 默认密码
	MinioBucket    = "evtvcr"
	ListenAddr     = ":9097"
	PresignExpire  = 10 * time.Minute
)

// ---------- S3 客户端初始化 ----------
func NewS3Client() *s3.Client {
	cfg, err := config.LoadDefaultConfig(
		context.TODO(),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(MinioAccessKey, MinioSecretKey, ""),
		),
		config.WithRegion("us-east-1"),
	)
	if err != nil {
		log.Fatalf("无法加载 AWS 配置: %v", err)
	}

	return s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(MinioEndpoint)
		o.UsePathStyle = true
	})
}

var s3Client *s3.Client

func main() {
	s3Client = NewS3Client()
	if err := ensureBucket(context.TODO(), s3Client, MinioBucket); err != nil {
		log.Fatalf("无法准备桶 %s: %v", MinioBucket, err)
	}

	http.HandleFunc("/upload", handleUpload)
	http.HandleFunc("/download", handleDownload)
	http.HandleFunc("/list", handleList)
	http.HandleFunc("/presign", handlePresign)
	http.HandleFunc("/delete", handleDelete)
	
	// 分片上传
	http.HandleFunc("/multipart/init", handleMultipartInit)
	http.HandleFunc("/multipart/complete", handleMultipartComplete)
	http.HandleFunc("/multipart/abort", handleMultipartAbort)

	log.Printf("HTTP Server 启动，监听 %s", ListenAddr)
	log.Fatal(http.ListenAndServe(ListenAddr, nil))
}

func ensureBucket(ctx context.Context, client *s3.Client, bucket string) error {
	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	if err == nil {
		log.Printf("已创建桶 %s", bucket)
		return nil
	}
	if isBucketAlreadyExists(err) {
		log.Printf("桶 %s 已存在，跳过创建", bucket)
		return nil
	}
	return err
}

func isBucketAlreadyExists(err error) bool {
	var alreadyOwned *types.BucketAlreadyOwnedByYou
	var alreadyExists *types.BucketAlreadyExists
	if errors.As(err, &alreadyOwned) || errors.As(err, &alreadyExists) {
		return true
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "BucketAlreadyOwnedByYou", "BucketAlreadyExists":
			return true
		}
	}
	return false
}

// ---------- 上传处理 ----------
func handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "仅支持 POST", http.StatusMethodNotAllowed)
		return
	}

	bucket := r.URL.Query().Get("bucket")
	key := r.URL.Query().Get("key")
	if bucket == "" || key == "" {
		http.Error(w, "缺少 bucket 或 key 参数", http.StatusBadRequest)
		return
	}

	// 读取请求体
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "读取请求体失败: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// 上传到 MinIO
	_, err = s3Client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(body),
	})
	if err != nil {
		http.Error(w, "上传失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	fmt.Fprintf(w, "上传成功: %s/%s\n", bucket, key)
}

// ---------- 下载处理 ----------
func handleDownload(w http.ResponseWriter, r *http.Request) {
	bucket := r.URL.Query().Get("bucket")
	key := r.URL.Query().Get("key")
	if bucket == "" || key == "" {
		http.Error(w, "缺少 bucket 或 key 参数", http.StatusBadRequest)
		return
	}

	// 从 MinIO 获取对象
	output, err := s3Client.GetObject(context.TODO(), &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		http.Error(w, "下载失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer output.Body.Close()

	// 将内容流式写回客户端
	w.Header().Set("Content-Type", "application/octet-stream")
	_, err = io.Copy(w, output.Body)
	if err != nil {
		log.Printf("写入响应失败: %v", err)
	}
}

// ---------- 列表处理 ----------
func handleList(w http.ResponseWriter, r *http.Request) {
	bucket := r.URL.Query().Get("bucket")
	if bucket == "" {
		http.Error(w, "缺少 bucket 参数", http.StatusBadRequest)
		return
	}

	output, err := s3Client.ListObjectsV2(context.TODO(), &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		http.Error(w, "列出对象失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "桶 %s 中的对象:\n", bucket)
	for _, obj := range output.Contents {
		keyStr := *obj.Key
		camID := strings.Split(keyStr, "/")[0]
		sessionID := strings.Split(keyStr, "/")[1]
		fileName := strings.Split(keyStr, "/")[2]
		fmt.Fprintf(w, "- %s (大小: %d 字节) -> %s\n", *obj.Key, obj.Size, camID + "|" + sessionID + "|" + fileName)
	}
	fmt.Fprintf(w, "共 %d 个文件\n", len(output.Contents))
}

// ---------- 预签名 URL ----------
func handlePresign(w http.ResponseWriter, r *http.Request) {
	camID := r.URL.Query().Get("camID")
	sessionID := r.URL.Query().Get("sessionID")
	fileName := r.URL.Query().Get("fileName")
	if camID == "" || sessionID == "" || fileName == "" {
		http.Error(w, "缺少 camID 或 sessionID 参数", http.StatusBadRequest)
		return
	}

	key := camID + "/" + sessionID + "/" + fileName
	presignClient := s3.NewPresignClient(s3Client)
	isDownload := r.URL.Query().Get("download") == "true" || r.URL.Query().Get("download") == "1"

	var outURL string
	if isDownload {
		out, err := presignClient.PresignGetObject(context.TODO(), &s3.GetObjectInput{
			Bucket: aws.String(MinioBucket),
			Key:    aws.String(key),
		}, s3.WithPresignExpires(PresignExpire))
		if err != nil {
			http.Error(w, "生成预签名url失败: "+err.Error(), http.StatusInternalServerError)
			return
		}
		outURL = out.URL
	} else if uploadID := r.URL.Query().Get("uploadId"); uploadID != "" {
		partNumber, err := strconv.ParseInt(r.URL.Query().Get("partNumber"), 10, 32)
		if err != nil || partNumber < 1 {
			http.Error(w, "缺少或非法 partNumber", http.StatusBadRequest)
			return
		}
		out, err := presignClient.PresignUploadPart(context.TODO(), &s3.UploadPartInput{
			Bucket:     aws.String(MinioBucket),
			Key:        aws.String(key),
			UploadId:   aws.String(uploadID),
			PartNumber: aws.Int32(int32(partNumber)),
		}, s3.WithPresignExpires(PresignExpire))
		if err != nil {
			http.Error(w, "生成分片预签名url失败: "+err.Error(), http.StatusInternalServerError)
			return
		}
		outURL = out.URL
	} else {
		out, err := presignClient.PresignPutObject(context.TODO(), &s3.PutObjectInput{
			Bucket: aws.String(MinioBucket),
			Key:    aws.String(key),
		}, s3.WithPresignExpires(PresignExpire))
		if err != nil {
			http.Error(w, "生成预签名url失败: "+err.Error(), http.StatusInternalServerError)
			return
		}
		outURL = out.URL
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, outURL)
}

func handleDelete(w http.ResponseWriter, r *http.Request) {
	bucket := r.URL.Query().Get("bucket")
	key := r.URL.Query().Get("key")
	if bucket == "" || key == "" {
		http.Error(w, "缺少 bucket 或 key 参数", http.StatusBadRequest)
		return
	}

	_, err := s3Client.DeleteObject(context.TODO(), &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		http.Error(w, "删除失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "删除成功: %s/%s\n", bucket, key)
}

func objectKeyFromQuery(r *http.Request) (string, bool) {
	camID := r.URL.Query().Get("camID")
	sessionID := r.URL.Query().Get("sessionID")
	fileName := r.URL.Query().Get("fileName")
	if camID == "" || sessionID == "" || fileName == "" {
		return "", false
	}
	return camID + "/" + sessionID + "/" + fileName, true
}

func handleMultipartInit(w http.ResponseWriter, r *http.Request) {
	key, ok := objectKeyFromQuery(r)
	if !ok {
		http.Error(w, "缺少 camID 或 sessionID 或 fileName 参数", http.StatusBadRequest)
		return
	}

	out, err := s3Client.CreateMultipartUpload(context.TODO(), &s3.CreateMultipartUploadInput{
		Bucket: aws.String(MinioBucket),
		Key:    aws.String(key),
	})
	if err != nil {
		http.Error(w, "初始化分片上传失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if out.UploadId == nil || *out.UploadId == "" {
		http.Error(w, "初始化分片上传失败: 空 uploadId", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, *out.UploadId)
}

func handleMultipartComplete(w http.ResponseWriter, r *http.Request) {
	key, ok := objectKeyFromQuery(r)
	if !ok {
		http.Error(w, "缺少 camID 或 sessionID 或 fileName 参数", http.StatusBadRequest)
		return
	}
	uploadID := r.URL.Query().Get("uploadId")
	if uploadID == "" {
		http.Error(w, "缺少 uploadId", http.StatusBadRequest)
		return
	}

	var parts []types.CompletedPart
	if err := json.NewDecoder(r.Body).Decode(&parts); err != nil {
		http.Error(w, "解析分片列表失败: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(parts) == 0 {
		http.Error(w, "分片列表为空", http.StatusBadRequest)
		return
	}

	_, err := s3Client.CompleteMultipartUpload(context.TODO(), &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(MinioBucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: parts,
		},
	})
	if err != nil {
		http.Error(w, "合并分片失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "分片上传完成: %s/%s\n", MinioBucket, key)
}

func handleMultipartAbort(w http.ResponseWriter, r *http.Request) {
	key, ok := objectKeyFromQuery(r)
	if !ok {
		http.Error(w, "缺少 camID 或 sessionID 或 fileName 参数", http.StatusBadRequest)
		return
	}
	uploadID := r.URL.Query().Get("uploadId")
	if uploadID == "" {
		http.Error(w, "缺少 uploadId", http.StatusBadRequest)
		return
	}

	_, err := s3Client.AbortMultipartUpload(context.TODO(), &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(MinioBucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
	})
	if err != nil {
		http.Error(w, "删除未完成分片失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "已中止分片上传并删除已上传分片: %s/%s\n", MinioBucket, key)
}
