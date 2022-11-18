package main

import (
  "fmt"
  cssh "golang.org/x/crypto/ssh"
  agent "golang.org/x/crypto/ssh/agent"
  "os"
  "io"
  "net"
  "time"
  "sync"
  "log"
  "strings"
)

func readBuffForString(sshOut io.Reader, prompt string) string {
  buf := make([]byte, 1000)
  // buf := make([]byte, 65535)
  n, err := sshOut.Read(buf) //this reads the ssh terminal
  waitingString := ""
  if err == nil {
    waitingString = string(buf[:n])
  }
  if len(waitingString) < len(prompt){
    prompt = prompt[len(prompt)-1:]
  }
  for waitingString[len(waitingString)-len(prompt):] != prompt {
    n, err = sshOut.Read(buf)
    waitingString += string(buf[:n])
    if err == io.EOF {
      // log.Printf("Normal exit (EOF).")
      break
    }

    if err != nil {
      log.Printf("Error readBuffForString: %#v\n",err)
      break
    }

    if len(waitingString) < len(prompt){
      prompt = prompt[len(prompt)-1:]
    }
  }
  return waitingString
}


func write(cmd string, sshIn io.WriteCloser) error {
  _, err := sshIn.Write([]byte(cmd + "\r"))
  return err
}

func write_bytes(cmd []byte, sshIn io.WriteCloser) (int, error) {
  n , err := sshIn.Write(cmd)
  return n, err
}

func ssh_collector(client Client, commands []string, wg *sync.WaitGroup, filenames *Output_file) {
      defer wg.Done()
      defer func() {
          if r := recover(); r != nil {
              log.Print("Recovered in [SC] ", r)
          }
      }()
      modes := cssh.TerminalModes{
          cssh.ECHO:          1,     // disable echoing
          cssh.TTY_OP_ISPEED: 115200, // input speed = 14.4kbaud
          cssh.TTY_OP_OSPEED: 115200, // output speed = 14.4kbaud
      }
      var (
        response string
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
      }else{
        authmethod = []cssh.AuthMethod{cssh.Password(client.Password)}
        // Cb := func(user, instruction string, questions []string, echos []bool) (answers []string, err error) {
        //     return []string{client.Password, client.Password}, nil
        // }
        // authmethod = []cssh.AuthMethod{cssh.RetryableAuthMethod(cssh.KeyboardInteractiveChallenge(Cb), 2)}
      }
      conn, err := cssh.Dial("tcp", fmt.Sprintf("%s:%d", client.Ip, client.Port), &cssh.ClientConfig{
        User:            client.User,
        Auth:            authmethod,
        HostKeyCallback: cssh.InsecureIgnoreHostKey(),
        Timeout:         time.Duration(conf.Common.Timeout) * time.Second,
      })

      if err != nil {
        log.Printf("Hostname %s error happened in ssh_collector: %#v\n %#v\n", client.Hostname, err, authmethod)
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
      t:=time.Now()
      date := fmt.Sprintf("%d%02d%02d_%02d%02d%02d", t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second())
      filename := fmt.Sprintf("%s%s_%s_%s.txt", conf.Common.Base_dir, strings.ToUpper(client.Profile), strings.ToUpper(client.Hostname), date)
      file, err := os.Create(filename)
      if err != nil{
          log.Printf("Host: %s Error: ", client.Hostname)
          log.Print("Unable to create file for saving output data from ssh node: %#v\n", err)
          return
      }
      filenames.Add(filename)

      waitingPrompt := client.Hostname+conf.Profiles[client.Profile].Unenable_prompt

      for _, cmd := range commands {
        if client.Profile == "Router" {
          if cmd[:3] == "rtr" {
            command := strings.Split(cmd, ":")

	    if command[1] == conf.Profiles[client.Profile].Enable_enter_command {
		    waitingPrompt = client.Hostname+conf.Profiles[client.Profile].Enable_prompt
	    }
	    if command[1] == conf.Profiles[client.Profile].Enable_exit_command {
		    waitingPrompt = client.Hostname+conf.Profiles[client.Profile].Unenable_prompt
	    }

            if conf.Common.Debug >= LOW {
		    log.Printf("%s:%s:%s:%s",client.Hostname,command[0],waitingPrompt,command[1])
            }
            write(command[1], sshIn)
            response = readBuffForString(sshOut, waitingPrompt)
            file.WriteString(response)
          }
        }
      }
      file.Close()
      session.Close()
      conn.Close()
}
