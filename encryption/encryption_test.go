package encryption

import (
	"encoding/hex"
	"testing"
)

func TestDecryptAndEncrypt(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{"Simple 1", "Text"},
		{"Simple 2", "Some random content"},
		{"Simple 3", "Lorem ipsum dolor sit amet, consetetur sadipscing elitr, sed diam nonumy eirmod tempor invidunt ut labore et dolore magna aliquyam erat, sed diam voluptua. At vero eos et accusam et justo duo dolores et ea rebum. Stet clita kasd gubergren, no sea takimata sanctus est Lorem ipsum dolor sit amet. Lorem ipsum dolor sit amet, consetetur sadipscing elitr, sed diam nonumy eirmod tempor invidunt ut labore et dolore magna aliquyam erat, sed diam voluptua."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, _ := hex.DecodeString("6368616e676520746869732070617373776f726420746f206120736563726574")
			cryptographer := InitEncrypter(key)

			text := tt.text

			fileId := cryptographer.GenerateIV(16)

			result := cryptographer.Encrypt([]byte(text), 1, fileId)
			decrypted := cryptographer.Decrypt(result, fileId, 1)

			plainStr := string(decrypted)
			if plainStr != text {
				t.Errorf("CryptoService.Decrypt() plain = %v, want = %v", plainStr, text)
				return
			}
		})
	}
}

func TestChunkingEncryptDecrypt(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{"SplitSimple 1", "Text"},
		{"SplitSimple 2", "Some random content"},
		{"SplitSimple 3", "Lorem ipsum dolor sit amet, consetetur sadipscing elitr, sed diam nonumy eirmod tempor invidunt ut labore et dolore magna aliquyam erat, sed diam voluptua. At vero eos et accusam et justo duo dolores et ea rebum. Stet clita kasd gubergren, no sea takimata sanctus est Lorem ipsum dolor sit amet. Lorem ipsum dolor sit amet, consetetur sadipscing elitr, sed diam nonumy eirmod tempor invidunt ut labore et dolore magna aliquyam erat, sed diam voluptua."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, _ := hex.DecodeString("6368616e676520746869732070617373776f726420746f206120736563726574")
			cryptographer := InitEncrypter(key)

			text := tt.text

			fileId := cryptographer.GenerateIV(16)
			textBytes := []byte(text)
			text1 := textBytes[:len(textBytes)/2]
			text2 := textBytes[len(textBytes)/2:]
			result1 := cryptographer.Encrypt([]byte(text1), 1, fileId)
			result2 := cryptographer.Encrypt([]byte(text2), 2, fileId)

			decrypted1 := cryptographer.Decrypt(result1, fileId, 1)
			decrypted2 := cryptographer.Decrypt(result2, fileId, 2)

			plainStr := string(decrypted1) + string(decrypted2)
			if plainStr != text {
				t.Errorf("CryptoService.Decrypt() plain = %v, want = %v", plainStr, text)
				return
			}
		})
	}
}
