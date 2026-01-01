package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
)

func GetRandomFileName(ext string) string {
	key := make([]byte, 32)
	rand.Read(key)
	return fmt.Sprintf("%s%s", base64.RawURLEncoding.EncodeToString(key), ext)
}

func areFloatsEqual(f1 float64, f2 float64, delta float64) bool {
	return math.Abs(f1-f2) <= delta
}

func getVideoAspectRatio(filePath string) (string, error) {
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return "", err
	}
	var m map[string]interface{}
	if err = json.Unmarshal(out.Bytes(), &m); err != nil {
		return "", err
	}
	streams, ok := m["streams"].([]interface{})
	if !ok {
		return "", errors.New("ffprobe output is not an array")
	}
	for _, s := range streams {
		stream := s.(map[string]interface{})
		if stream["codec_type"].(string) != "video" {
			continue
		}
		width := stream["width"].(float64)
		height := stream["height"].(float64)
		if areFloatsEqual(width/height, 16.0/9.0, 0.005) {
			return "16:9", nil
		}
		if areFloatsEqual(width/height, 9.0/16.0, 0.005) {
			return "9:16", nil
		}
		return "other", nil
	}
	return "", errors.New("no video stream in ffprobe output")
}

func getVideoNamePrefix(filePath string) (string, error) {
	ratio, err := getVideoAspectRatio(filePath)
	if err != nil {
		return "", err
	}
	switch ratio {
	case "16:9":
		return "landscape", nil
	case "9:16":
		return "portrait", nil
	default:
		return ratio, nil
	}
}

func processVideoForFastStart(filePath string) (string, error) {
	dir, fn := filepath.Split(filePath)
	outPath := filepath.Join(dir, fmt.Sprintf("ff_%s", fn))
	cmd := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", outPath)
	err := cmd.Run()
	if err != nil {
		return "", err
	}
	return outPath, nil
}

func generatePresignedURL(s3Client *s3.Client, bucket, key string, expireTime time.Duration) (string, error) {
	presignedClient := s3.NewPresignClient(s3Client)
	obj := s3.GetObjectInput{Bucket: &bucket, Key: &key}
	req, err := presignedClient.PresignGetObject(context.Background(), &obj, s3.WithPresignExpires(expireTime))
	if err != nil {
		return "", err
	}
	return req.URL, nil
}

func (cfg *apiConfig) _dbVideoToSignedVideo(video database.Video) (database.Video, error) {
	urlParts := strings.Split(*video.VideoURL, ",")
	res, err := generatePresignedURL(cfg.s3Client, urlParts[0], urlParts[1], time.Minute)
	if err != nil {
		return video, err
	}
	video.VideoURL = &res
	return video, nil
}
