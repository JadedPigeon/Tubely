package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

type ffprobeStreams struct {
	Streams []struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	} `json:"streams"`
}

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

	fmt.Println("uploading video", videoID, "by user", userID)

	const maxMemory = 1 << 30
	r.ParseMultipartForm(maxMemory)

	multipartfile, multipartheader, err := r.FormFile("video")
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
	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusUnsupportedMediaType, "Only video/mp4 is supported", nil)
		return
	}

	metadata, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to get video metadata", err)
		return
	}

	if metadata.UserID != userID {
		respondWithError(w, http.StatusForbidden, "You do not have permission to upload a video for this video", nil)
		return
	}

	videoFile, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to create temporary file", err)
		return
	}
	defer os.Remove(videoFile.Name())
	defer videoFile.Close()

	_, err = io.Copy(videoFile, multipartfile)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to copy video file", err)
		return
	}

	_, err = videoFile.Seek(0, io.SeekStart)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to seek in video file", err)
		return
	}

	// Determine the aspect ratio of the video
	aspectRatio, err := getVideoAspectRatio(videoFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to determine video aspect ratio", err)
		return
	}
	var prefix string
	switch aspectRatio {
	case "16:9":
		prefix = "landscape"
	case "9:16":
		prefix = "portrait"
	default:
		prefix = "other"
	}

	// Generate a random 32-byte hex string for the S3 key
	randomBytes := make([]byte, 32)
	_, err = rand.Read(randomBytes)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to generate random S3 key", err)
		return
	}
	randomHex := hex.EncodeToString(randomBytes)
	s3Key := fmt.Sprintf("%s/%s.mp4", prefix, randomHex)

	_, err = cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      aws.String(cfg.s3Bucket),
		Key:         aws.String(s3Key),
		Body:        videoFile,
		ContentType: &mediaType,
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to upload video to S3", err)
		return
	}

	metadata.VideoURL = aws.String(fmt.Sprintf("https://%s.s3.amazonaws.com/%s", cfg.s3Bucket, s3Key))
	err = cfg.db.UpdateVideo(metadata)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to update video metadata", err)
		return
	}

	respondWithJSON(w, http.StatusOK, map[string]string{"url": *metadata.VideoURL})
}

func getVideoAspectRatio(filePath string) (string, error) {
	arguments := "-v error -print_format json -show_streams " + filePath
	cmd := exec.Command("ffprobe", strings.Split(arguments, " ")...)
	var output bytes.Buffer
	cmd.Stdout = &output
	err := cmd.Run()
	if err != nil {
		return "", err
	}

	var probe ffprobeStreams
	err = json.Unmarshal(output.Bytes(), &probe)
	if err != nil {
		return "", err
	}
	// determine the ratio, then returned one of three strings: 16:9, 9:16, or other
	if len(probe.Streams) == 0 || probe.Streams[0].Height == 0 {
		return "", fmt.Errorf("no video stream with width and height found")
	}

	const tolerance = 0.01

	for _, stream := range probe.Streams {
		if stream.Width > 0 && stream.Height > 0 {
			w := float64(stream.Width)
			h := float64(stream.Height)
			ratio := w / h
			if abs(ratio-16.0/9.0) < tolerance {
				return "16:9", nil
			}
			if abs(ratio-9.0/16.0) < tolerance {
				return "9:16", nil
			}
			return "other", nil
		}
	}
	return "", fmt.Errorf("no video stream with width and height found")
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
