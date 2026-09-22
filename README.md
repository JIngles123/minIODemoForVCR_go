# minIODemoForVCR_go

功能：go实现的，与MinIO交互的文件上传下载Demo，终端程序

Demo细分的功能点：
1. 查询当前所有待上传的文件列表
2. 查询当前所有上传成功的文件列表 -> 对本地文件的核查，已上传的会在上传成功后，在文件或整体session后+uploaded标记
3. 查询MinIO上已有的文件列表
4. 删除MinIO上的指定文件
5. 输入要上传的session文件(camID|sessionID|fileName), 直接上传，大文件不考虑切片上传
6. 输入要上传的session文件(camID|sessionID|fileName), 获取预签名url，大文件(>2MB)时考虑切片上传
7. 输入要下载的session文件(camID|sessionID|fileName), 直接下载
8. 输入要下载的session文件(camID|sessionID|fileName), 获取预签名url

测试：

client:
go run client/main.go

AC250071|1789112960|thumbnail-1789112960.jpg

上传：
curl --upload-file ./EvtvcrForTest/AC250071/1789112960/thumbnail-1789112960.jpg \
  'http://localhost:9000/evtvcr/RTSPCH02/1789113609/vectors.idx?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=jlmtest%2F20260921%2Fus-east-1%2Fs3%2Faws4_request&X-Amz-Date=20260921T124554Z&X-Amz-Expires=600&X-Amz-SignedHeaders=host&x-id=PutObject&X-Amz-Signature=9c4926148e109150c9eb587c178fa8ec9cb510780b90d37944a06e09c2048502'

========================================================

server:
go run server/main.go

下载：
$ curl -o a.txt 'http://localhost:9000/evtvcr/AC250071/1789112034/thumbnail-1789112034.jpg?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Checksum-Mode=ENABLED&X-Amz-Credential=jlmtest%2F20260922%2Fus-east-1%2Fs3%2Faws4_request&X-Amz-Date=20260922T033305Z&X-Amz-Expires=600&X-Amz-SignedHeaders=host&x-id=GetObject&X-Amz-Signature=5daf81ffac5d83eff77e07421109e8a5e5b3a07188fb8b8a38bb8f2a78523ce5'

