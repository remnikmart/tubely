package main

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	// Reading video data from DB
	videoMetaData, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, 400, "", err)
		return
	}
	if videoMetaData.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "Not user's video", err)
		return
	}

	fmt.Println("uploading video", videoID, "by user", userID)

	// Fetching video
	var maxMemory int64 = 10 << 30
	r.Body = http.MaxBytesReader(w, r.Body, maxMemory)
	err = r.ParseMultipartForm(maxMemory)
	if err != nil {
		respondWithError(w, http.StatusRequestEntityTooLarge, "", err)
		return
	}
	file, fileHeader, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "", err)
		return
	}
	defer file.Close()
	fileMediaType := fileHeader.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(fileMediaType)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "", err)
		return
	}
	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "unsupported file MIME format", err)
		return
	}

	// Save video to temporary file
	dst, err := os.CreateTemp("/tmp", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "", err)
		return
	}
	defer os.Remove(dst.Name())
	defer dst.Close()
	_, err = io.Copy(dst, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "", err)
		return
	}

	// Moving moov atom for fast streaming
	optimizedFileName, err := processVideoForFastStart(dst.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "", err)
		return
	}
	defer os.Remove(optimizedFileName)

	// Send file to S3
	uploadFile, err := os.Open(optimizedFileName)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "", err)
		return
	}
	prefix, err := getVideoNamePrefix(optimizedFileName)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "", err)
		return
	}
	storedFileName := fmt.Sprintf("%s/%s", prefix, GetRandomFileName(filepath.Ext(fileHeader.Filename)))
	s3params := s3.PutObjectInput{Bucket: &cfg.s3Bucket, Key: &storedFileName, Body: uploadFile, ContentType: &mediaType}
	_, err = cfg.s3Client.PutObject(context.Background(), &s3params)
	if err != nil {
		respondWithError(w, 400, "", err)
		return
	}

	//Update video metadata in db
	fileUrl := fmt.Sprintf("https://%s/%s", cfg.s3CfDistribution, storedFileName)
	videoMetaData.VideoURL = &fileUrl
	err = cfg.db.UpdateVideo(videoMetaData)
	if err != nil {
		respondWithError(w, 400, "", err)
		return
	}

	respondWithJSON(w, http.StatusOK, videoMetaData)
}
