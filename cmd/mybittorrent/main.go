package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"time"

	"net/http"
	"net/url"
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
}

func main() {
	command := os.Args[1]
	switch command {
	case "decode":
		bencodedValue := os.Args[2]

		decoded, err := decodeBencode(bencodedValue)
		if err != nil {
			fmt.Println(err)
			return
		}

		jsonOutput, _ := json.Marshal(decoded)
		fmt.Println(string(jsonOutput))
	case "info":
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
	case "peers":
		filePath := os.Args[2]
		parsedTorrent, err := parseTorrentFile(filePath)
		if err != nil {
			fmt.Println(err)
			return
		}
		peers, err := getPeers(parsedTorrent)
		for i := range peers {
			fmt.Println(peers[i])
		}
	case "handshake":
		filePath := os.Args[2]
		parsedTorrent, err := parseTorrentFile(filePath)
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println("Peer ID:", peerHandshake(parsedTorrent, os.Args[3]))
	default:
		fmt.Println("Unknown command: " + command)
		os.Exit(1)
	}

}

type TorrentPeers struct {
	Interval int
	Peers    string
}

func hexToASCII(hexStr string) (string, error) {
	bytes, err := hex.DecodeString(hexStr)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func getPeers(parsedTorrent ParsedTorrent) ([]string, error) {
	var result = make([]string, 0)
	baseURL := parsedTorrent.Tracker
	//encodes the InfoHash to ascii
	asciiStr, err := hexToASCII(parsedTorrent.InfoHash)
	if err != nil {
		fmt.Println("Error transforming hex to ascii,", err)
	}
	urlEncondedAsciiStr := url.QueryEscape(asciiStr)

	params := url.Values{}
	params.Add("peer_id", "00112233445566778889")
	params.Add("port", "6881")
	params.Add("uploaded", "0")
	params.Add("downloaded", "0")
	params.Add("left", strconv.FormatInt(parsedTorrent.Length, 10))
	params.Add("compact", "1")
	fullURL := baseURL + "?" + params.Encode() + "&info_hash=" + urlEncondedAsciiStr
	resp, err := http.Get(fullURL)
	defer resp.Body.Close()
	if err != nil {
		fmt.Println("error making the get request:", err)
	}
	var trakcerResponse TorrentPeers
	err = bencode.Unmarshal(resp.Body, &trakcerResponse)
	if err != nil {
		fmt.Println("Error on unmarshal", err)
	}
	for i := 0; i < len(trakcerResponse.Peers); i += 6 {
		// high value + low value
		port := int(trakcerResponse.Peers[i+4])<<8 + int(trakcerResponse.Peers[i+5])
		peer := fmt.Sprintf("%d.%d.%d.%d:%s",
			trakcerResponse.Peers[i],
			trakcerResponse.Peers[i+1],
			trakcerResponse.Peers[i+2],
			trakcerResponse.Peers[i+3],
			strconv.Itoa(port))
		result = append(result, peer)
	}

	return result, nil
}

func peerHandshake(parsedTorrent ParsedTorrent, address string) string {
	infoHash, err := hexToASCII(parsedTorrent.InfoHash)
	protocolLength := byte(19)
	concatenatedBytes := append([]byte{protocolLength}, []byte("BitTorrent protocol")...)
	concatenatedBytes = append(concatenatedBytes, make([]byte, 8)...)
	concatenatedBytes = append(concatenatedBytes, []byte(infoHash)...)
	concatenatedBytes = append(concatenatedBytes, []byte("00112233445566778889")...)

	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		fmt.Println("Error connecting to server,", err)
	}
	defer conn.Close()

	_, err = conn.Write(concatenatedBytes)
	if err != nil {
		fmt.Println("Error sending concatenated bytes:", err)
	}

	buffer := make([]byte, 68)
	_, err = conn.Read(buffer)
	if err != nil {
		fmt.Println("Error reading data:", err)
	}
	hexRepresentation := hex.EncodeToString(buffer[len(buffer)-20:])

	return hexRepresentation
}
