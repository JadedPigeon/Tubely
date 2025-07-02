package main

// streak
import (
	"fmt"
	"io"
	"net/http"

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

	mediaType := multipartheader.Header.Get("Content-Type")
	if mediaType == "" {
		respondWithError(w, http.StatusBadRequest, "Missing media type", nil)
		return
	}
	thumbnailData, err := io.ReadAll(multipartfile)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to read thumbnail data", err)
		return
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

	videoThumbnails[videoID] = thumbnail{
		data:      thumbnailData,
		mediaType: mediaType,
	}

	url := "http://localhost:8091/api/thumbnails/" + videoIDString
	metadata.ThumbnailURL = &url

	//print the thumbnail
	fmt.Printf("Thumbnail uploaded for video %s by user %s, URL: %s\n", videoID, userID, url)

	err = cfg.db.UpdateVideo(metadata)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to update video metadata", err)
		return
	}

	respondWithJSON(w, http.StatusOK, metadata)
}
