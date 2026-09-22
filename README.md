# minIODemoForVCR_go

测试命令：

client:
go run client/main.go

1
----------------------------
2
-----------------------------
3

AC250071|1789112960|thumbnail-1789112960.jpg

cmd上传：
curl --upload-file ./EvtvcrForTest/AC250071/1789112960/thumbnail-1789112960.jpg \
  'http://localhost:9000/evtvcr/RTSPCH02/1789113609/vectors.idx?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=jlmtest%2F20260921%2Fus-east-1%2Fs3%2Faws4_request&X-Amz-Date=20260921T124554Z&X-Amz-Expires=600&X-Amz-SignedHeaders=host&x-id=PutObject&X-Amz-Signature=9c4926148e109150c9eb587c178fa8ec9cb510780b90d37944a06e09c2048502'

========================================================

server:
go run server/main.go


