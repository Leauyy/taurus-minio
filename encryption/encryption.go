package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"io"
)

type Cryptographer struct {
	gcm cipher.AEAD
}

// Constructor for cryptography system
// Takes byte array key and procudes GCM cipher
func InitEncrypter(key []byte) *Cryptographer {
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err.Error())
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		panic(err.Error())
	}

	return &Cryptographer{
		gcm: aesgcm,
	}
}

// Helper function to generate random number of a given length
func (cryptographer *Cryptographer) GenerateIV(length int) []byte {
	nonce := make([]byte, length)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		panic(err.Error())
	}
	return nonce
}

// Helper function that converts uint64 to 8 byte array
func (cryptographer *Cryptographer) idToBytes(id uint64) []byte {
	block := make([]byte, 8)
	binary.LittleEndian.PutUint64(block, id)

	return block
}

// encrypts byte[] appends file ID block number and IV
// https://nuetzlich.net/gocryptfs/forward_mode_crypto/
// Encrypted file structure:
// filed id
// ....
// blockId
// IV
// encrypted contents
// -----
// Size = encrypted block size + 8bytes(uint64) + 16bytes(fileId) = +20 whenreading
// Integrity is preserved by blockId and fileId which are used in GCM as additional data
func (cryptographer Cryptographer) Encrypt(filePart []byte, blockId uint64, fileId []byte) []byte {

	// convert blockId to bytes
	block := cryptographer.idToBytes(blockId)

	// Use this for GCM verification
	additional_data := make([]byte, 8)
	copy(additional_data, block)
	additional_data = append(additional_data, fileId...)
	// Generate IV
	iv := cryptographer.GenerateIV(12)

	cypherBytes := cryptographer.gcm.Seal(nil, iv, filePart, additional_data)

	return append(iv, cypherBytes...)
}

// Decrypt byte stream of:
// IV + Cypher text
// Takes cypherText `filePart` and decrypts it
// `blockId` and `fileId` are used again for preserving integrity when decrypting
func (cryptographer Cryptographer) Decrypt(filePart []byte, fileId []byte, blockId uint64) []byte {
	// Convert to bytes and create additional data for GCM
	block := cryptographer.idToBytes(blockId)
	additional_data := append(block, fileId...)

	// Fetch stored IV for the block
	iv := filePart[:12]

	// Actual cyphertext
	dataBytes := filePart[12:]

	decryptedBytes, err := cryptographer.gcm.Open(nil, iv, dataBytes, additional_data)

	if err != nil {
		panic(err.Error())
	}
	return decryptedBytes
}
