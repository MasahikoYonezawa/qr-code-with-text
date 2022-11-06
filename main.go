package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/qr"
	"github.com/golang/freetype/truetype"
	imageFont "golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
	"image"
	"image/draw"
	"image/png"
	"log"
	"math"
	"net/http"
	netURL "net/url"
	"os"
	"strconv"
	"time"
	"unicode/utf8"
)

const (
	FontFilePath          = "./bin/mgenplus-2cp-regular.ttf"
	ErrTextRequiredParams = "the 'url' or 'label' query string parameter is required"
	ErrTextLabelMaxLength = "`label` character count over"
	LabelMaxLength        = 10
	QRSizeDefault         = 300
	QRSizeMax             = 1000
	QRSizeMin             = 150
	RateBaseImageHeight   = 1.1
	RateLabelHeight       = 1.075
	RateFontSize          = 0.1
)

type QRCode struct {
	URL   string
	Size  int
	Image barcode.Barcode
}

func NewQRCode(reqURL string, size int) (*QRCode, error) {
	// URLのデコード
	url, decodeErr := netURL.QueryUnescape(reqURL)
	if decodeErr != nil {
		return nil, decodeErr
	}
	// QRコード生成
	encoded, encodeErr := qr.Encode(url, qr.M, qr.Auto)
	if encodeErr != nil {
		return nil, encodeErr
	}
	// サイズ調整
	scaled, scaleErr := barcode.Scale(encoded, size, size)
	if scaleErr != nil {
		return nil, scaleErr
	}
	return &QRCode{
		URL:   url,
		Size:  size,
		Image: scaled,
	}, nil
}

type BaseImage struct {
	Width      int
	Height     int
	BackGround *image.RGBA
}

func NewBaseImage(size int) (*BaseImage, error) {
	bgWidth := size
	bgHeight := int(math.Round(float64(size) * RateBaseImageHeight))
	// 背景
	backGround := image.NewRGBA(image.Rect(0, 0, bgWidth, bgHeight))
	draw.Draw(backGround, backGround.Bounds(), image.White, image.Point{}, draw.Src)

	return &BaseImage{
		Width:      bgWidth,
		Height:     bgHeight,
		BackGround: backGround,
	}, nil
}

func (b BaseImage) addLabel(label Label) {
	// テキスト描画の設定
	d := &imageFont.Drawer{
		Dst:  b.BackGround,
		Src:  image.Black,
		Face: label.Font,
		Dot:  fixed.Point26_6{},
	}
	// 描画位置
	d.Dot.X = (fixed.I(b.Width) - d.MeasureString(label.Text)) / 2
	d.Dot.Y = fixed.I(int(math.Round(float64(b.Width) * RateLabelHeight)))
	// 描画
	d.DrawString(label.Text)
}

func (b BaseImage) synthesizeImage(qrCode QRCode) {
	// QRコードとラベル付きの背景画像を合成
	rct := image.Rectangle{Min: image.Point{X: 0, Y: 0}, Max: b.BackGround.Bounds().Size()}
	draw.Draw(b.BackGround, rct, qrCode.Image, image.Point{}, draw.Src)
}

func (b BaseImage) toBase64() (string, error) {
	// pngにエンコード
	var buf bytes.Buffer
	pngErr := png.Encode(&buf, b.BackGround)
	if pngErr != nil {
		return "", pngErr
	}

	imageBinary := buf.Bytes()
	buf.Reset()
	return base64.StdEncoding.EncodeToString(imageBinary), nil
}

type Label struct {
	Text string
	Font imageFont.Face
}

func NewLabel(reqLabel string, size int) (*Label, error) {
	font, nfErr := NewFont(size)
	if nfErr != nil {
		return nil, nfErr
	}
	return &Label{
		Text: reqLabel,
		Font: font,
	}, nil
}

func NewFont(size int) (imageFont.Face, error) {
	// フォントファイル読み込み
	ftBinary, rfErr := os.ReadFile(FontFilePath)
	if rfErr != nil {
		return nil, rfErr
	}
	ft, pErr := truetype.Parse(ftBinary)
	if pErr != nil {
		return nil, pErr
	}

	fontSize := math.Floor(float64(size) * RateFontSize)
	opt := truetype.Options{
		Size:              fontSize,
		DPI:               0,
		Hinting:           0,
		GlyphCacheEntries: 0,
		SubPixelsX:        0,
		SubPixelsY:        0,
	}
	return truetype.NewFace(ft, &opt), nil
}

func Handler(request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	var emptyAGW *events.APIGatewayProxyResponse
	// パラメータチェック
	if errRes := validate(request); errRes != nil {
		return *errRes, nil
	}
	size := decideSize(request.QueryStringParameters["size"])
	// QRコードの生成
	qrCode, nqErr := NewQRCode(request.QueryStringParameters["url"], size)
	if nqErr != nil {
		log.Printf("EVENT1: %s", nqErr)
		return *emptyAGW, nqErr
	}
	// ラベル情報生成
	label, nlErr := NewLabel(request.QueryStringParameters["label"], size)
	if nlErr != nil {
		log.Printf("EVENT2: %s", nlErr)
		return *emptyAGW, nlErr
	}
	// 背景画像作成
	baseImage, nbiErr := NewBaseImage(size)
	if nbiErr != nil {
		log.Printf("EVENT3: %s", nbiErr)
		return *emptyAGW, nbiErr
	}
	// ラベルを背景につける
	baseImage.addLabel(*label)
	// 画像を合成する
	baseImage.synthesizeImage(*qrCode)
	// base64にする
	resBody, tbErr := baseImage.toBase64()
	if tbErr != nil {
		log.Printf("EVENT4: %s", tbErr)
		return *emptyAGW, tbErr
	}

	statusCode := 200
	cacheFrom := time.Now().Format(http.TimeFormat)
	cacheUntil := time.Now().AddDate(1, 0, 0).Format(http.TimeFormat)
	mimetype := "image/png"
	return events.APIGatewayProxyResponse{
		Headers: map[string]string{
			"Content-Type":  mimetype,
			"Last-Modified": cacheFrom,
			"Expires":       cacheUntil,
		},
		Body:            resBody,
		StatusCode:      statusCode,
		IsBase64Encoded: true,
	}, nil
}

func validate(request events.APIGatewayProxyRequest) (errRes *events.APIGatewayProxyResponse) {
	// クエリパラメータチェック
	if request.QueryStringParameters["url"] == "" || request.QueryStringParameters["label"] == "" {
		log.Printf("validate EVENT1: %s", errors.New(ErrTextRequiredParams))
		return &events.APIGatewayProxyResponse{
			Body:       fmt.Sprintf("{Message: %s}", "クエリパラメータの`url`と`label`は必須です"),
			StatusCode: 400,
		}
	}
	if utf8.RuneCountInString(request.QueryStringParameters["label"]) > LabelMaxLength {
		log.Printf("validate EVENT2: %s", errors.New(ErrTextLabelMaxLength))
		return &events.APIGatewayProxyResponse{
			Body:       fmt.Sprintf("{Message: %s}", "`label`は10文字以内で指定してください"),
			StatusCode: 400,
		}
	}
	return nil
}

func decideSize(reqSize string) int {
	size, parseIntErr := strconv.Atoi(reqSize)
	if parseIntErr != nil {
		return QRSizeDefault
	}
	if size > QRSizeMax {
		return QRSizeMax
	}
	if size < QRSizeMin {
		return QRSizeMin
	}
	return size
}

func main() {
	lambda.Start(Handler)
}
