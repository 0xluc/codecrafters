package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
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
	case "download_piece":
		var torrentFile, outputPath string
		if os.Args[2] == "-o" {
			torrentFile = os.Args[4]
			outputPath = os.Args[3]
		} else {
			torrentFile = os.Args[4]
			outputPath = "."
		}
		parsedTorrent, err := parseTorrentFile(torrentFile)
		if err != nil {
			fmt.Println(err)
			return
		}
		ind, _ := strconv.Atoi(os.Args[5])
		peers, err := getPeers(parsedTorrent)
		if err != nil {
			fmt.Println(err)
		}
		data := downloadFile(parsedTorrent, peers[0], ind)

		file, err := os.Create(outputPath)
		if err != nil {
			fmt.Println(err)
		}
		defer file.Close()
		_, err = file.Write(data)
		if err != nil {
			fmt.Println(err)
		}
		fmt.Printf("Piece downloaded to %s.\n", outputPath)
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

type PeerMessage struct {
	lengthPrefix uint32
	id           uint8
	index        uint32
	begin        uint32
	length       uint32
}

func downloadFile(parsedTorrent ParsedTorrent, address string, index int) []byte {
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

	buffer := make([]byte, 1024)
	_, err = conn.Read(buffer)
	if err != nil {
		fmt.Println("error reading", err)
	}

	// wait for the bitfield message id=5
	buf := make([]byte, 4)
	_, err = conn.Read(buf)
	if err != nil {
		fmt.Println(err)
	}
	peerMessage := PeerMessage{}
	peerMessage.lengthPrefix = binary.BigEndian.Uint32(buf)
	payloadBuf := make([]byte, peerMessage.lengthPrefix)
	_, err = conn.Read(payloadBuf)
	if err != nil {
		fmt.Println(err)
	}
	peerMessage.id = payloadBuf[0]

	fmt.Printf("Received message: %v\n", peerMessage.id)
	if peerMessage.id != 5 {
		fmt.Println("Bitfield message not found")
	}

	// send 2
	_, err = conn.Write([]byte{0, 0, 0, 1, 2})
	if err != nil {
		fmt.Println(err)
	}
	//wait for 1
	buf = make([]byte, 4)
	_, err = conn.Read(buf)
	if err != nil {
		fmt.Println(err)
	}
	peerMessage = PeerMessage{}
	peerMessage.lengthPrefix = binary.BigEndian.Uint32(buf)
	payloadBuf = make([]byte, peerMessage.lengthPrefix)
	_, err = conn.Read(payloadBuf)
	if err != nil {
		fmt.Println(buf)
	}
	peerMessage.id = payloadBuf[0]
	fmt.Printf("Received message: %v\n", peerMessage.id)
	if peerMessage.id != 1 {
		fmt.Println(buf)
		fmt.Println("Unckoke message not found")
	}

	// request 6
	pieceSize := parsedTorrent.PieceLength
	pieceCnt := int(math.Ceil(float64(parsedTorrent.Length) / float64(pieceSize)))
	if index == pieceCnt-1 {
		pieceSize = parsedTorrent.Length % parsedTorrent.PieceLength
	}
	blockSize := 16 * 1024
	blockCnt := int(math.Ceil(float64(pieceSize) / float64(blockSize)))
	fmt.Printf("File Length: %d, Piece Length: %d, Piece Count: %d, Block Size: %d, Block Count: %d\n", parsedTorrent.Length, parsedTorrent.PieceLength, pieceCnt, blockSize, blockCnt)
	var data []byte
	for i := 0; i < blockCnt; i++ {
		blockLength := blockSize
		if i == blockCnt-1 {
			blockLength = int(pieceSize) - ((blockCnt - 1) * int(blockSize))
		}
		peerMessage := PeerMessage{
			lengthPrefix: 13,
			id:           6,
			index:        uint32(index),
			begin:        uint32(i * int(blockSize)),
			length:       uint32(blockLength),
		}
		var buf bytes.Buffer
		binary.Write(&buf, binary.BigEndian, peerMessage)
		_, err = conn.Write(buf.Bytes())
		if err != nil {
			fmt.Println(err)
		}
		fmt.Println("Sent request message", peerMessage.id)
		resBuf := make([]byte, 4)
		_, err = conn.Read(resBuf)
		if err != nil {
			fmt.Println(err)
		}
		peerMessage = PeerMessage{}
		peerMessage.lengthPrefix = binary.BigEndian.Uint32(resBuf)
		payloadBuf := make([]byte, peerMessage.lengthPrefix)
		_, err = io.ReadFull(conn, payloadBuf)
		if err != nil {
			fmt.Println(err)
		}
		peerMessage.id = payloadBuf[0]
		fmt.Printf("Received message: %v\n", peerMessage.id)
		data = append(data, payloadBuf[9:]...)
	}
	return data
}
