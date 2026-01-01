package main

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func fileNameFromURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	return path.Base(u.Path), nil
}

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
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

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	// Fetching image data
	var maxMemory int64 = 10 << 20
	err = r.ParseMultipartForm(int64(maxMemory))
	if err != nil {
		respondWithError(w, http.StatusRequestEntityTooLarge, "", err)
		return
	}

	file, fileHeader, err := r.FormFile("thumbnail")
	fileMediaType := fileHeader.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(fileMediaType)
	if err != nil {
		respondWithError(w, 400, "", err)
		return
	}
	if mediaType != "image/jpeg" && mediaType != "image/png" {
		respondWithError(w, 400, "unsupported file MIME format", err)
		return
	}
	imageData, err := io.ReadAll(file)
	if err != nil {
		respondWithError(w, 400, "", err)
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

	// Store image file to disk
	storedFileName := GetRandomFileName(filepath.Ext(fileHeader.Filename))
	storedFilePath := filepath.Join(cfg.assetsRoot, storedFileName)
	f, err := os.Create(storedFilePath)
	if err != nil {
		respondWithError(w, 400, "", err)
		return
	}
	defer f.Close()
	_, err = io.Copy(f, bytes.NewReader(imageData))

	//Update video metadata in db
	thumbUrl := fmt.Sprintf("http://localhost:%s/assets/%s", cfg.port, storedFileName)
	oldThumbnailUrl := videoMetaData.ThumbnailURL
	videoMetaData.ThumbnailURL = &thumbUrl
	err = cfg.db.UpdateVideo(videoMetaData)
	if err != nil {
		respondWithError(w, 400, "", err)
		return
	}

	// Delete old file
	oldFileName, err := fileNameFromURL(*oldThumbnailUrl)
	if err == nil {
		os.Remove(filepath.Join(cfg.assetsRoot, oldFileName))
	}

	respondWithJSON(w, http.StatusOK, videoMetaData)
}
