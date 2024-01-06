package main

import (
	"log"
	"taurus-minio/client"
	"taurus-minio/encryption"
	"taurus-minio/files"

	"github.com/gin-gonic/gin"
)

func main() {
	// Create minio client and read configuration files
	minioClient := client.CreateMinioClient()
	log.Printf("Is Online: %t\n", minioClient.IsOnline())
	// Typically for first start, if no bucket is present.
	minioClient.CreateBucket()
	// Create cryptographer for encrypting/decrypting
	cryptographer := encryption.InitEncrypter(minioClient.GetEncryptionKey())

	// Create file handler, responsible for receiving/sending/chunking/encrypting files
	fh := files.InitFileHandler(minioClient, cryptographer)

	// start gin
	router := gin.Default()
	router.POST("/upload/file", fh.UploadFilesHandler) // upload a file
	router.GET("/file/:name", fh.GetFileFromIDHandler) // get file by id
	router.Run(":8080")
}
