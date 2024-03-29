package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	cssh "golang.org/x/crypto/ssh"
	agent "golang.org/x/crypto/ssh/agent"
)

func readSSHInputFile(sshOut io.Reader, prompt *regexp.Regexp, file *os.File) []byte {
	buf := make([]byte, 65535)
	var waitingString bytes.Buffer

	n, err := sshOut.Read(buf) //this reads the ssh terminal
	if err == nil {
		waitingString.Write(buf[:n])
	}
	for !prompt.Match(waitingString.Bytes()) {

		if waitingString.Len() > 1024*1024*10 { // every 10MB of data from the node will be truncated to the file
			_, err = file.Write(waitingString.Bytes())
			if err == nil {
				waitingString.Reset()
			}
		}

		n, err = sshOut.Read(buf)
		waitingString.Write(buf[:n])
		if err == io.EOF {
			// log.Printf("Normal exit (EOF).")
			break
		}
		if err != nil {
			log.Printf("Error readSSHInputFile: %#v\n", err)
			break
		}
		time.Sleep(time.Duration(conf.Common.Timeout_read) * time.Nanosecond)
	}
	return waitingString.Bytes()
}

func readSSHInput(sshOut io.Reader, prompt *regexp.Regexp) []byte {
	buf := make([]byte, 65535)
	var waitingString bytes.Buffer

	n, err := sshOut.Read(buf) //this reads the ssh terminal
	if err == nil {
		waitingString.Write(buf[:n])
	}
	for !prompt.Match(waitingString.Bytes()) {

		n, err = sshOut.Read(buf)
		waitingString.Write(buf[:n])
		if err == io.EOF {
			// log.Printf("Normal exit (EOF).")
			break
		}
		if err != nil {
			log.Printf("Error readSSHInput: %#v\n", err)
			break
		}
		time.Sleep(time.Duration(conf.Common.Timeout_read) * time.Nanosecond)
	}
	return waitingString.Bytes()
}

func write(cmd string, sshIn io.WriteCloser) error {
	_, err := sshIn.Write([]byte(cmd + "\r"))
	return err
}

func write_bytes(cmd []byte, sshIn io.WriteCloser) (int, error) {
	n, err := sshIn.Write(cmd)
	return n, err
}

func ssh_collector(client Client, commands map[string][]string, wg *sync.WaitGroup, filenames *Output_file) {
	defer wg.Done()
	defer func() {
		if r := recover(); r != nil {
			log.Print("Recovered in [SC] ", r)
		}
	}()
	modes := cssh.TerminalModes{
		cssh.ECHO:          1,      // disable echoing
		cssh.TTY_OP_ISPEED: 115200, // input speed = 14.4kbaud
		cssh.TTY_OP_OSPEED: 115200, // output speed = 14.4kbaud
	}
	var (
		response   []byte
		authmethod []cssh.AuthMethod
	)

	if conf.Auth.One_for_all {
		client.User = conf.Auth.Username
		client.Password = conf.Auth.Password
	}

	if conf.Auth.Pubkey {
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
		pass, err := base64.StdEncoding.DecodeString(client.Password)
		if err != nil {
			log.Fatalf("Cannot decode password of node %s err: %s", client.Hostname, err)
		}
		authmethod = []cssh.AuthMethod{cssh.Password(string(pass))}
	}

	var myConfig cssh.Config
	myConfig.SetDefaults()
	myConfig.Ciphers = append(myConfig.Ciphers, "aes128-cbc", "aes192-cbc", "aes256-cbc", "3des-cbc")
	myConfig.MACs = []string{"hmac-sha2-256", "hmac-sha1", "hmac-sha1-96", "hmac-sha2-256-etm@openssh.com"} //TODO: this is a WA - https://github.com/golang/go/issues/32075

	sshConfig := cssh.ClientConfig{
		User:            client.User,
		Auth:            authmethod,
		ClientVersion:   "SSH-2.0-MyCollector2.0",
		HostKeyCallback: cssh.InsecureIgnoreHostKey(),
		Timeout:         time.Duration(conf.Common.Timeout) * time.Second,
		Config:          myConfig,
	}

	if conf.Common.Debug >= LOW+1 {
		log.Printf("%+v\n", sshConfig)
	}
	conn, err := cssh.Dial("tcp", fmt.Sprintf("%s:%d", client.Ip, client.Port), &sshConfig)

	if err != nil {
		log.Printf("Hostname %s error happened in ssh_collector: %#v\n", client.Hostname, err)
		return
	}
	session, err := conn.NewSession()
	handleError(err)
	sshOut, err := session.StdoutPipe()
	handleError(err)
	sshIn, err := session.StdinPipe()
	err = session.RequestPty("vt100", 0, 0, modes)
	handleError(err)
	err = session.Shell()
	handleError(err)
	t := time.Now()
	date := fmt.Sprintf("%d%02d%02d_%02d%02d%02d", t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second())
	filename := fmt.Sprintf("%s%s_%s_%s.txt", conf.Common.Base_dir, strings.ToUpper(client.Profile), strings.ToUpper(client.Hostname), date)
	file, err := os.Create(filename)
	if err != nil {
		log.Printf("Host: %s Error: ", client.Hostname)
		log.Printf("Unable to create file for saving output data for node: %#v\n", err)
		return
	}
	filenames.Add(filename)

	waitingPromptRg, _ := regexp.Compile(fmt.Sprintf("%s.*%s", client.Hostname, conf.Profiles[client.Profile].Unenable_prompt))
	waitBracket := fmt.Sprintf("%s.*%s", client.Hostname, conf.Profiles[client.Profile].Unenable_prompt)

	for _, cmd := range commands[client.Profile] {

		if cmd == conf.Profiles[client.Profile].Enable_enter_command {
			waitingPromptRg, _ = regexp.Compile(fmt.Sprintf("%s.*%s", client.Hostname, conf.Profiles[client.Profile].Enable_prompt))
			waitBracket = fmt.Sprintf("%s.*%s", client.Hostname, conf.Profiles[client.Profile].Enable_prompt)
		}
		if cmd == conf.Profiles[client.Profile].Enable_exit_command {
			waitingPromptRg, _ = regexp.Compile(fmt.Sprintf("%s.*%s", client.Hostname, conf.Profiles[client.Profile].Unenable_prompt))
			waitBracket = fmt.Sprintf("%s.*%s", client.Hostname, conf.Profiles[client.Profile].Unenable_prompt)
		}

		if conf.Common.Debug >= LOW {
			log.Printf("%s:%s:%s", client.Hostname, waitBracket, cmd)
		}
		write(cmd, sshIn)
		response = readSSHInputFile(sshOut, waitingPromptRg, file)
		file.Write(response)
	}
	file.Close()
	session.Close()
	conn.Close()
}
