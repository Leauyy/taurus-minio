package files

import (
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"taurus-minio/client"
	"taurus-minio/encryption"

	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"
)

// TODO: move to config
const BUFFER_SIZE uint64 = 16384

// Parses chunk size from string and returns number of bytes to process in each chunk
func parseChunkSize(size string) (uint64, error) {
	r, _ := regexp.Compile("([0-9]+)([A-z]*)B")
	matches := r.FindStringSubmatch(size)
	log.Printf("Matches %s \n", matches)
	if len(matches) != 3 {
		log.Fatalln("Matches should be 3")
		return 0, errors.New("Chunk size must be in format digit + size. E.g. 1MB")
	}
	mult, err := strconv.Atoi(matches[1])
	if err != nil {
		log.Fatalf("Cant convert %s\n to integer", matches[1])
		return 0, errors.New("Chunk size digit must be integer")
	}

	number := float64(mult)

	scale := matches[2]

	fac := float64(1)

	switch scale {
	case "K":
		fac = math.Pow(10, 3)
	case "M":
		fac = math.Pow(10, 6)
	case "G":
		fac = math.Pow(10, 9)
	case "T":
		fac = math.Pow(10, 12)
	case "P":
		fac = math.Pow(10, 15)
	default:
		fac = 1
	}
	// Dealing with whole numbers
	return uint64(float64(number) * fac), nil
}

// Helper function to understand chunk size after encryption. Should change if different encryption is used
func getEncryptionOverhead() uint64 {
	return 12 + 16
}

// Helper function that takes file name and chunkId and produces name for chunk. Should be changed for better chunk naming algorithm
func getChunkName(filename string, id uint64) string {
	return fmt.Sprintf("%s_chunk%d", filename, id)
}

type FileHandler struct {
	minioClient   *client.MinioClient
	cryptographer *encryption.Cryptographer
}

// Creates File Handler, responsible for handling file upload/download
func InitFileHandler(minioClient *client.MinioClient, cryptographer *encryption.Cryptographer) *FileHandler {
	return &FileHandler{
		minioClient:   minioClient,
		cryptographer: cryptographer,
	}
}

// Upload wrapper, takes reader and fileName
// Encrypts the content received on file
// and uploads the encrypted content
func (fh *FileHandler) uploadFileWrapper(file io.Reader, filename string) (minio.UploadInfo, error) {
	r, w := io.Pipe()
	defer r.Close()
	go fh.readEncryptWrite(file, w)

	return fh.minioClient.UploadFile(r, filename)
}

// Main handler for uploading files
// Uses gin context to retrieve data
func (fh *FileHandler) UploadFilesHandler(c *gin.Context) {
	// Fetch the file, dont read it and start stream go routine
	file, header, err := c.Request.FormFile("upload")
	if err != nil {
		log.Fatalln(err)
	}
	filename := header.Filename

	// No chunk usage. Simple upload/download
	if !fh.minioClient.UseChunking() {
		info, errUpload := fh.uploadFileWrapper(file, filename)
		if errUpload != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"message": errUpload,
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"ETag":   info.ETag,
		})
	} else {
		// Use chunks enabled, get chunk size from file options
		chunkSize := c.Request.FormValue("chunk-size")
		log.Printf("Chunking file in size of %s \n", chunkSize)
		byteSize, err := parseChunkSize(chunkSize)
		if err != nil {
			// Return error if unable to parse chunkSize
			c.JSON(http.StatusInternalServerError, gin.H{
				"message": err,
			})
			return
		}
		// Create a chunk while reading
		// As a "lazy" solution we just pipe bytes again for the chunk size
		chunkId := uint64(0)
		var chunkTags []string

		chunkBufferSize := BUFFER_SIZE
		// For very small chunks.
		if byteSize < chunkBufferSize {
			chunkBufferSize = byteSize / 4
		}

		// Read from buffer.
		// Check if fits in chunk
		// Otherwise read as much as fits
		outBuf := make([]byte, chunkBufferSize)
		isEof := false
		for {
			chunkName := getChunkName(filename, chunkId)
			// 16 bytes of fileid
			currentChunkSize := uint64(16)
			nextBlock := uint64(0)
			r_chunk, w_chunk := io.Pipe()

			go func() {
				// launch go routine
				// read chunk until size:
				for {
					if byteSize <= currentChunkSize {
						break
					}
					// Last data of chunk. Checks if we can handle another full buffer + encryption overhead
					spaceForNextWrite := byteSize - currentChunkSize
					n := 0
					var err error
					// Overhead of 44 bytes on a chunk for simplicity sake(Assumes file chunks are of big sizes mostly)
					// IV = 12 byte
					// Fileid = 16 byte
					// ACM = 16 byte
					if spaceForNextWrite < chunkBufferSize {
						smallBuff := make([]byte, spaceForNextWrite)
						n, err = file.Read(smallBuff)
						if err != nil && err != io.EOF {
							log.Fatalln(err)
						}
						copy(outBuf, smallBuff)
					} else {
						// Normal read
						n, err = file.Read(outBuf)
						if err != nil && err != io.EOF {
							log.Fatalln(err)
						}
					}

					// The actual size on disk after encryption. Use this to keep track of chunk sizes
					currentChunkSize += uint64(n) + getEncryptionOverhead()
					w_chunk.Write(outBuf[:n])

					if err == io.EOF {
						isEof = true
						break
					}

					nextBlock++
				}
				// Closes chunk writer. Chunk reader reads EOF
				w_chunk.Close()
			}()

			info, errUpload := fh.uploadFileWrapper(r_chunk, chunkName)
			chunkId++
			// Close chunk as it read EOF when it returned from wrapper
			r_chunk.Close()

			if errUpload != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"message": "Error uploading chunk",
				})
				return
			}
			chunkTags = append(chunkTags, info.ETag)

			// Can only happen after w_chunk.Close() is called
			if isEof {
				log.Printf("Finished processing all chunks")
				break
			}
		}

		// Response to client
		c.JSON(http.StatusOK, gin.H{
			"status": fmt.Sprintf("Successfully uploaded %d chunks", chunkId-1),
			"Tags":   chunkTags,
		})
	}
}

// File retrieval handler
// Retrieves file with a given uri parameter on file/`name`
func (fh *FileHandler) GetFileFromIDHandler(c *gin.Context) {
	name := c.Param("name")

	// Create pipe, used for both chunk and non chunk modes
	r, w := io.Pipe()
	defer r.Close()

	if !fh.minioClient.UseChunking() {
		reader := fh.minioClient.DownloadFile(name)
		defer reader.Close()

		go fh.readDecryptWrite(reader, w)

	} else {
		// Retrieve a list of chunks. Max of 1000 chunks supported
		chunks := fh.minioClient.GetAllChunks(name)
		chunkCount := len(chunks)
		if chunkCount == 0 {
			c.JSON(http.StatusNotFound, gin.H{
				"message": "Could not find file",
			})
			return
		}
		// TODO: move to config
		routineCount := 8
		chunkers := make(map[int]chan []byte)

		// Start routines and create their channels of size 1 for orderly deliver
		for j := 0; j < routineCount; j++ {
			// TODO: test with bigger channels if possible for routines to download more chunks
			chunkers[j] = make(chan []byte, 1)
			count := chunkCount / routineCount
			remainder := chunkCount - count*routineCount
			if remainder > j {
				count += 1
			}
			go fh.retrieveAllChunks(j, routineCount, count, name, chunkers[j])
		}

		// Ordered delivery. Retrieve from each chunk channel which is blocking.
		go func() {
			for i := 0; i < chunkCount; i++ {
				log.Printf("Reading chunk %d\n", i)
				routineId := i % routineCount
				data := <-chunkers[routineId]
				w.Write(data)
			}
			w.Close()
		}()
	}

	// resulting file name
	extraHeaders := map[string]string{
		"Content-Disposition": fmt.Sprintf(`attachment; filename="%s"`, name),
	}

	// Reader response. Will start serving part of response as soon as womething is written to `w`(Writer)
	c.DataFromReader(http.StatusOK, -1, "application/octet-stream", r, extraHeaders)
}

// Single go routine code for retrieving the chunks it is reponsible for
// Routine id is a number [0-routineCount)
// Chunk count is the number of chunks this routine will have to retrieve
// Name is the file name of the chunked-file we are trying to retrieve
// Result should be a channel of byte array of size 1.
//
// So given 3 routines and 7 chunks:
// routine 1 (id=0) will fetch chunks: 1,4,7
// routine 2 (id=1) will fetch chunks: 2,5
// routine 3 (id=1) will fetch chunks: 3,6
// The chunks are written to a channel of size 1 which is not fetching next chunk until current is read
func (fh *FileHandler) retrieveAllChunks(id, routineCount, chunkCount int, name string, result chan []byte) {
	log.Printf("Retreiving %d chunks. Id: %d", chunkCount, id)
	for i := 0; i < chunkCount; i++ {
		chunkId := uint(i*routineCount + id)
		chunkName := getChunkName(name, uint64(chunkId))
		log.Printf("Downloading chunk: %s", chunkName)
		chunkReader := fh.minioClient.DownloadFile(chunkName)

		r, w := io.Pipe()

		go fh.readDecryptWrite(chunkReader, w)
		var chunkBuff []byte
		readBuf := make([]byte, BUFFER_SIZE)
		for {
			n, err := r.Read(readBuf)
			if err != nil && err != io.EOF {
				log.Fatalf("%s,%s \n", name, err)
			}
			chunkBuff = append(chunkBuff, readBuf[:n]...)
			if err == io.EOF {
				break
			}
		}
		r.Close()
		log.Printf("Decrypted chunk %d\n", chunkId)
		// write to channel for retrieval
		result <- chunkBuff
	}
}

// function to decrypt the current file being read.
// Reads data from the reader
// Reads fileId first for decryption additional data
// Reads encrypted file content
// Writer writes the decrypted data
// Once file is processed writer is closed which sends EOF to the underlying PipeReader
func (fh *FileHandler) readDecryptWrite(reader io.Reader, w *io.PipeWriter) {
	defer w.Close()

	// Read file ID first for decryption
	fileId := make([]byte, 16)
	_, err := reader.Read(fileId)

	if err != nil && err != io.EOF {
		log.Fatalln(err)
	}

	// Read file size + IV(12bytes) + AES GCM 16 Bytes
	// https://stackoverflow.com/questions/67028762/why-aes-256-with-gcm-adds-16-bytes-to-the-ciphertext-size
	outBuf := make([]byte, BUFFER_SIZE+12+16)
	// count blocks for integrity check
	blockId := uint64(0)
	for {
		n, err := reader.Read(outBuf)
		if err != nil && err != io.EOF {
			log.Fatalln(err)
		}

		//decrypt here
		decryptedBytes := fh.cryptographer.Decrypt(outBuf[:n], fileId, blockId)
		if n > 0 {
			w.Write(decryptedBytes)
		}
		if err == io.EOF {
			break
		}
		blockId++
	}
}

// Function to encrypt current file being read.
// Generates unique file id of 16bytes
// Reads "plaintext" from `file`
// Encrypts the data
// writes the encrypted data to pipe writer
// Closed writer signals that encryption is done and reader has reached EOF
func (fh *FileHandler) readEncryptWrite(file io.Reader, w *io.PipeWriter) {
	defer w.Close()
	// Generate unique file ID
	fileId := fh.cryptographer.GenerateIV(16)
	w.Write(fileId)

	// count blocks for integrity check
	nextBlock := uint64(0)
	outBuf := make([]byte, BUFFER_SIZE)
	for {
		n, err := file.Read(outBuf)
		if err != nil && err != io.EOF {
			log.Fatalln(err)
		}

		if err == io.EOF || n == 0 {
			break
		}
		// Encrypt here
		encrypted_text := fh.cryptographer.Encrypt(outBuf[:n], nextBlock, fileId)
		w.Write(encrypted_text)
		nextBlock++
	}
	w.Close()
}
