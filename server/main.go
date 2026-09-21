package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// ---------- 配置 ----------
const (
	MinioEndpoint  = "http://localhost:9090" // 你的 MinIO 地址
	MinioAccessKey = "jlmtest"               // 默认用户名
	MinioSecretKey = "jlmtestpwd"            // 默认密码
	MinioBucket    = "evtvcr"
	ListenAddr     = ":9097"
	PresignExpire  = 15 * time.Minute
)

// ---------- S3 客户端初始化 ----------
func NewS3Client() *s3.Client {
	cfg, err := config.LoadDefaultConfig(context.TODO(),
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

	http.HandleFunc("/upload", handleUpload)
	http.HandleFunc("/download", handleDownload)
	http.HandleFunc("/list", handleList)
	http.HandleFunc("/presign", handlePresign)

	log.Printf("HTTP Server 启动，监听 %s", ListenAddr)
	log.Fatal(http.ListenAndServe(ListenAddr, nil))
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
		fmt.Fprintf(w, "- %s (大小: %d 字节)\n", *obj.Key, obj.Size)
	}
}

// ---------- 预签名 URL（上传 PUT，整个 session 一个对象，不做切片） ----------
func handlePresign(w http.ResponseWriter, r *http.Request) {
	camID := r.URL.Query().Get("camID")
	sessionID := r.URL.Query().Get("sessionID")
	if camID == "" || sessionID == "" {
		http.Error(w, "缺少 camID 或 sessionID 参数", http.StatusBadRequest)
		return
	}

	key := camID + "/" + sessionID
	presignClient := s3.NewPresignClient(s3Client)
	out, err := presignClient.PresignPutObject(context.TODO(), &s3.PutObjectInput{
		Bucket: aws.String(MinioBucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(PresignExpire))
	if err != nil {
		http.Error(w, "生成预签名url失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, out.URL)
}
