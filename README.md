# qr-code-with-text
QRコードにテキストを付与して生成するアプリ

## Usage

1. Compile function

```
cd qr_with_text_go
go get github.com/aws/aws-lambda-go 
go get github.com/boombuler/barcode 
go get github.com/golang/freetype/truetype
GOOS=linux go build -o bin/main
// For Apple silicon
GOARCH=amd64 GOOS=linux go build -o bin/main

serverless plugin install -n serverless-apigw-binary   
```

2. Deploy!

```
serverless deploy --aws-profile [profile] --stage [stage]
```
