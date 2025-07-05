package main

// streak
import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

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

	const maxMemory = 10 << 20
	r.ParseMultipartForm(maxMemory)

	multipartfile, multipartheader, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer multipartfile.Close()

	mediaTypeHeader := multipartheader.Header.Get("Content-Type")
	if mediaTypeHeader == "" {
		respondWithError(w, http.StatusBadRequest, "Missing media type", nil)
		return
	}
	mediaType, _, err := mime.ParseMediaType(mediaTypeHeader)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid media type", err)
		return
	}

	// Only allow image/jpeg and image/png
	if mediaType != "image/jpeg" && mediaType != "image/png" {
		respondWithError(w, http.StatusBadRequest, "Only JPEG and PNG thumbnails are allowed", nil)
		return
	}
	// this is a comment

	exts, _ := mime.ExtensionsByType(mediaType)
	ext := ""
	if len(exts) > 0 {
		ext = exts[0]
	} else {
		ext = ".png" // fallback
	}

	metadata, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to get video metadata", err)
		return
	}

	if metadata.UserID != userID {
		respondWithError(w, http.StatusForbidden, "You do not have permission to upload a thumbnail for this video", nil)
		return
	}

	// Use the videoID to create a unique file path. filepath.Join and cfg.assetsRoot will be helpful here
	filepath := "assets/" + videoIDString + ext
	systemfile, err := os.Create(filepath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to create system file for thumbnail", err)
	}
	defer systemfile.Close()

	_, err = io.Copy(systemfile, multipartfile)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to write thumbnail to file", err)
		return
	}

	fileURL := "http://localhost:8091/assets/" + videoIDString + ext

	metadata.ThumbnailURL = &fileURL

	//print the thumbnail
	fmt.Printf("Thumbnail uploaded for video %s by user %s, URL: is too long", videoID, userID)

	err = cfg.db.UpdateVideo(metadata)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to update video metadata", err)
		return
	}

	respondWithJSON(w, http.StatusOK, metadata)
}
