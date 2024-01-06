package client

import (
	"context"
	"encoding/hex"
	"io"
	"log"

	"github.com/BurntSushi/toml"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Config struct {
	Minio MinioConfiguration
}

type MinioConfiguration struct {
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	UseSSL          bool
	BucketName      string
	EncryptionKey   string
	Chunking        bool
}

type MinioClient struct {
	client        *minio.Client
	configuration *MinioConfiguration
	ctx           context.Context
}

func readConfiguration() *MinioConfiguration {
	// Read the TOML file
	var conf Config
	_, err := toml.DecodeFile("config.toml", &conf)
	if err != nil {
		log.Fatalln(err)
	}

	return &conf.Minio
}

func CreateMinioClient() *MinioClient {
	// Create context
	ctx := context.Background()
	// Read configuration
	conf := readConfiguration()
	// Initialize minio client object.
	minioClient, err := minio.New(conf.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(conf.AccessKeyID, conf.SecretAccessKey, ""),
		Secure: conf.UseSSL,
	})
	if err != nil {
		log.Fatalln(err)
	}

	log.Printf("%#v\n", minioClient) // minioClient is now setup

	// Build MinioClient
	_minioClient := MinioClient{
		client:        minioClient,
		configuration: conf,
		ctx:           ctx,
	}
	return &_minioClient
}

func (minioClient *MinioClient) UseChunking() bool {
	return minioClient.configuration.Chunking
}

func (minioClient *MinioClient) GetEncryptionKey() []byte {
	key, err := hex.DecodeString(minioClient.configuration.EncryptionKey)
	if err != nil {
		log.Fatalln(err)
	}
	return key
}

func (minioClient *MinioClient) CreateBucket() {
	// Pass empty options as we only need name
	err := minioClient.client.MakeBucket(minioClient.ctx, minioClient.configuration.BucketName, minio.MakeBucketOptions{})
	if err != nil {
		log.Printf("Failed to create bucket. Checking if %s bucket exists", minioClient.configuration.BucketName)
		// Check to see if we already own this bucket (which happens if you run this twice)
		exists, errBucketExists := minioClient.client.BucketExists(minioClient.ctx, minioClient.configuration.BucketName)
		if errBucketExists == nil && exists {
			log.Printf("We already own %s\n", minioClient.configuration.BucketName)
		} else {
			log.Fatalln(err)
		}
	} else {
		log.Printf("Successfully created %s\n", minioClient.configuration.BucketName)
	}

}

func (minioClient *MinioClient) IsOnline() bool {
	return minioClient.client.IsOnline()
}

func (minioClient *MinioClient) DownloadFile(name string) *minio.Object {
	reader, err := minioClient.client.GetObject(minioClient.ctx, minioClient.configuration.BucketName, name, minio.GetObjectOptions{})
	if err != nil {
		log.Fatalf("Error downloading image %s, error: %s\n", name, err)
	}

	return reader
}

// Max 1000chunks
func (minioClient *MinioClient) GetAllChunks(name string) []string {
	chunkName := name + "_"
	objectCh := minioClient.client.ListObjects(minioClient.ctx, minioClient.configuration.BucketName, minio.ListObjectsOptions{Prefix: chunkName})

	chunks := make([]string, 0)
	for object := range objectCh {
		if object.Err != nil {
			log.Fatalln(object.Err)
			return chunks
		}
		chunks = append(chunks, object.Key)
	}
	return chunks
}

func (minioClient *MinioClient) UploadFile(file io.Reader, fileName string) (minio.UploadInfo, error) {

	info, err := minioClient.client.PutObject(minioClient.ctx, minioClient.configuration.BucketName, fileName, file, -1, minio.PutObjectOptions{ContentType: "application/octet-stream"})
	if err != nil {
		log.Fatalln(err)
	}
	log.Printf("Successfully uploaded %s of size %d\n", fileName, info.Size)
	return info, nil
}
