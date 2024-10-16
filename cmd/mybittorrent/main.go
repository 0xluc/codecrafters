package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/jackpal/bencode-go"
)

type ParsedTorrent struct {
	Tracker     string
	Length      int64
	InfoHash    string
	PieceLength int64
	PieceHashes []string
}

// Example:
// - 5:hello -> hello
// - 10:hello12345 -> hello12345
func decodeBencode(bencodedString string) (interface{}, error) {
	reader := strings.NewReader(bencodedString)

	data, err := bencode.Decode(reader)
	if err != nil {
		return "", err
	}
	return data, nil
}

func hashBuffer(buffer bytes.Buffer) string {
	hasher := sha1.New()
	hasher.Write(buffer.Bytes())
	hashed := hasher.Sum(nil)
	return hex.EncodeToString(hashed)
}

func parseTorrentFile(location string) (ParsedTorrent, error) {
	parsedTorrent := &ParsedTorrent{}

	data, err := os.ReadFile(location)
	if err != nil {
		return *parsedTorrent, err
	}
	decodedData, err := decodeBencode(string(data))
	if resultMap, ok := decodedData.(map[string]interface{}); ok {
		if announce, ok := resultMap["announce"].(string); ok {
			parsedTorrent.Tracker = announce
		}
	}

	if resultMap, ok := decodedData.(map[string]interface{}); ok {
		// marshal the info element again
		var buffer bytes.Buffer
		infoDict := resultMap["info"]
		if err := bencode.Marshal(&buffer, infoDict); err != nil {
			fmt.Println("Error marshaling", err)
			return *parsedTorrent, err
		}
		parsedTorrent.InfoHash = hashBuffer(buffer)

		// get the length
		if info, ok := resultMap["info"].(map[string]interface{}); ok {
			if l, ok := info["length"].(int64); ok {
				parsedTorrent.Length = l
			}
			if piece, ok := info["piece length"].(int64); ok {
				parsedTorrent.PieceLength = piece
			}
			if pieces, ok := info["pieces"].(string); ok {
				chunkSize := 20
				for i := 0; i < len(pieces); i += chunkSize {
					end := i + chunkSize
					if end > len(pieces) {
						end = len(pieces)
					}
					chunk := pieces[i:end]
					parsedTorrent.PieceHashes = append(parsedTorrent.PieceHashes, hex.EncodeToString([]byte(chunk)))
				}
			}
		}
	}
	if err != nil {
		return *parsedTorrent, err
	}
	return *parsedTorrent, nil

	//if unicode.IsDigit(rune(bencodedString[0])) {
	//	var firstColonIndex int

	//	for i := 0; i < len(bencodedString); i++ {
	//		if bencodedString[i] == ':' {
	//			firstColonIndex = i
	//			break
	//		}
	//	}

	//	lengthStr := bencodedString[:firstColonIndex]

	//	length, err := strconv.Atoi(lengthStr)
	//	if err != nil {
	//		return "", err
	//	}

	//	return bencodedString[firstColonIndex+1 : firstColonIndex+1+length], nil
	//}
}

func main() {

	command := os.Args[1]

	if command == "decode" {

		bencodedValue := os.Args[2]

		decoded, err := decodeBencode(bencodedValue)
		if err != nil {
			fmt.Println(err)
			return
		}

		jsonOutput, _ := json.Marshal(decoded)
		fmt.Println(string(jsonOutput))
	} else if command == "info" {
		filePath := os.Args[2]
		parsedTorrent, err := parseTorrentFile(filePath)
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println("Tracker URL:", parsedTorrent.Tracker)
		fmt.Println("Length:", parsedTorrent.Length)
		fmt.Println("Info Hash:", parsedTorrent.InfoHash)
		fmt.Println("Piece Length:", parsedTorrent.PieceLength)
		fmt.Println("Piece Hashes:")
		for i := range parsedTorrent.PieceHashes {
			fmt.Println(parsedTorrent.PieceHashes[i])
		}
	} else {
		fmt.Println("Unknown command: " + command)
		os.Exit(1)
	}
}
