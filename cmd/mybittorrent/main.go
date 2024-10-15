package main

import (
	"encoding/json"
	"fmt"
	"github.com/jackpal/bencode-go"
	"os"
	"strings"
)

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
func parseTorrentFile(location string) (string, int64, error) {
	data, err := os.ReadFile(location)
	var tracker string
	var length int64
	if err != nil {
		return "", 0, err
	}
	decodedData, err := decodeBencode(string(data))
	if resultMap, ok := decodedData.(map[string]interface{}); ok {
		if announce, ok := resultMap["announce"].(string); ok {
			tracker = announce
		}
	}

	if resultMap, ok := decodedData.(map[string]interface{}); ok {
		if info, ok := resultMap["info"].(map[string]interface{}); ok {
			if l, ok := info["length"].(int64); ok {
				length = l
			}
		}
	}
	if err != nil {
		return "", 0, err
	}
	return tracker, length, nil

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
		tracker, length, err := parseTorrentFile(filePath)
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println("Tracker URL:", tracker)
		fmt.Println("Length:", length)
	} else {
		fmt.Println("Unknown command: " + command)
		os.Exit(1)
	}
}
