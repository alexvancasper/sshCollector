package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/pkg/sftp"
	cssh "golang.org/x/crypto/ssh"
	agent "golang.org/x/crypto/ssh/agent"

	// "crypto/md5"

	"math/rand"
)

func handleError(err error) {
	if err != nil {
		panic(err)
	}
}

// func GetMD5Hash(text string) string {
//     hasher := md5.New()
//     hasher.Write([]byte(text))
//     return hex.EncodeToString(hasher.Sum(nil))
// }

func copy(src, dst string) (int64, error) {
	sourceFileStat, err := os.Stat(src)
	if err != nil {
		return 0, err
	}

	if !sourceFileStat.Mode().IsRegular() {
		return 0, fmt.Errorf("%s is not a regular file", src)
	}

	source, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	defer destination.Close()
	nBytes, err := io.Copy(destination, source)
	return nBytes, err
}

func read_command_file(filename string) ([]string, error) {
	var commands []string
	file, err := os.Open(filename)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		text := scanner.Text()
		if len(text) > 0 && text[:1] != "#" {
			commands = append(commands, scanner.Text())
		}
	}
	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}
	return commands, err
}

func upload_on_remote_scp(sourcefile string, Remote *Upload) bool {
	var authmethod []cssh.AuthMethod
	if Remote.Pubkey {
		if conf.Common.Debug >= MEDIUM {
			log.Print("SFTP: Authentication by pubkey")
		}
		sock, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK"))
		if err != nil {
			log.Fatal(err)
		}
		agent_ := agent.NewClient(sock)
		signers, err := agent_.Signers()
		if err != nil {
			log.Fatal(err)
		}
		authmethod = []cssh.AuthMethod{cssh.PublicKeys(signers...)}
	} else {
		if conf.Common.Debug >= MEDIUM {
			log.Print("SFTP: Authentication by password")
		}
		password_bytes, err := base64.StdEncoding.DecodeString(Remote.Password)
		handleError(err)
		authmethod = []cssh.AuthMethod{cssh.Password(string(password_bytes))}
	}

	conn, err := cssh.Dial("tcp", fmt.Sprintf("%s:%d", Remote.Server, Remote.Port), &cssh.ClientConfig{
		User:            Remote.Username,
		Auth:            authmethod,
		HostKeyCallback: cssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	handleError(err)
	if conf.Common.Debug >= MEDIUM {
		log.Print("SFTP: Connected")
	}

	client, err := sftp.NewClient(conn)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	if Remote.Path[len(Remote.Path)-1:] != "/" {
		Remote.Path += "/"
	}
	dstFile, err := client.Create(Remote.Path + filepath.Base(sourcefile))
	handleError(err)
	defer dstFile.Close()

	srcFile, err := os.Open(sourcefile)
	handleError(err)

	bytes, err := io.Copy(dstFile, srcFile)
	handleError(err)
	log.Printf("%d bytes copied\n", bytes)
	return true

}

func download_from_remote_scp(sourcefile string, destinatiog_folder string, client Client) bool {
	defer func() {
		if r := recover(); r != nil {
			log.Print("Recovered in [download_from_remote_scp] ", r)
		}
	}()
	var authmethod []cssh.AuthMethod

	if conf.Auth.One_for_all {
		client.User = conf.Auth.Username
		client.Password = conf.Auth.Password
	}

	if conf.Auth.Pubkey {
		if conf.Common.Debug >= MEDIUM {
			log.Print("[Download]: Authentication by pubkey")
		}
		sock, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK"))
		if err != nil {
			log.Fatal(err)
		}
		agent_ := agent.NewClient(sock)
		signers, err := agent_.Signers()
		if err != nil {
			log.Fatal(err)
		}
		authmethod = []cssh.AuthMethod{cssh.PublicKeys(signers...)}
	} else {
		if conf.Common.Debug >= MEDIUM {
			log.Print("[Download]: Authentication by password")
		}
		authmethod = []cssh.AuthMethod{cssh.Password(client.Password)}
	}
	conn, err := cssh.Dial("tcp", fmt.Sprintf("%s:%d", client.Ip, client.Port), &cssh.ClientConfig{
		User:            client.User,
		Auth:            authmethod,
		HostKeyCallback: cssh.InsecureIgnoreHostKey(),
		Timeout:         time.Duration(conf.Common.Timeout) * time.Second,
	})
	handleError(err)

	if conf.Common.Debug >= MEDIUM {
		log.Print("[Download]: Connected")
	}
	remote, err := sftp.NewClient(conn)
	if err != nil {
		log.Fatal(err)
	}
	defer remote.Close()

	if destinatiog_folder[len(destinatiog_folder)-1:] != "/" {
		destinatiog_folder += "/"
	}

	dstFile, err := os.Create(destinatiog_folder + client.Hostname + ".cfg")
	handleError(err)
	defer dstFile.Close()

	srcFile, err := remote.Open(sourcefile)
	handleError(err)

	bytes, err := io.Copy(dstFile, srcFile)
	handleError(err)
	log.Printf("%d bytes copied\n", bytes)
	return true

}

func postFile(filename, targetUrl, fieldname string) bool {
	bodyBuf := &bytes.Buffer{}
	bodyWriter := multipart.NewWriter(bodyBuf)

	// this step is very important
	fileWriter, err := bodyWriter.CreateFormFile(fieldname, filename)
	handleError(err)

	// open file handle
	fh, err := os.Open(filename)
	handleError(err)
	defer fh.Close()

	//iocopy
	_, err = io.Copy(fileWriter, fh)
	handleError(err)

	contentType := bodyWriter.FormDataContentType()
	bodyWriter.Close()
	resp, err := http.Post(targetUrl, contentType, bodyBuf)
	handleError(err)

	defer resp.Body.Close()
	resp_body, err := ioutil.ReadAll(resp.Body)
	handleError(err)
	log.Printf("Postfile status: %s\n", resp.Status)
	log.Print(string(resp_body))
	return true
}

func sendToPlugin() {
	fmt.Println("sendToPlugin")
	return
}

func Random() int {
	rand.Seed(time.Now().UnixNano())
	min := 1000
	max := 3000
	return rand.Intn(max-min) + min
}
