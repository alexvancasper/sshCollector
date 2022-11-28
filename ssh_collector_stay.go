package main

import (
	"encoding/json"
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

func ssh_collector_constant(client Client, commands []string, wg *sync.WaitGroup, filenames *Output_file, channel chan string, Threads *States, id int, plugins map[string]Plugin, inbound Inbound) error {
	defer log.Print("[SCC] done")
	defer wg.Done()
	// defer Threads.SetStatus(id, NEW)
	defer func() {
		if r := recover(); r != nil {
			log.Print("Recovered in [ssh_collector_constant] ", r)
		}
	}()

	type output_format struct {
		nodename string `json:"nodename"`
		ip       string `json:"ip"`
		command  string `json:"command"`
		output   string `json:"output"`
	}

	var (
		response   []byte
		authmethod []cssh.AuthMethod
		wg_func    sync.WaitGroup
		file       *os.File
		filename   string = ""
		// json_output output_format
	)

	modes := cssh.TerminalModes{
		cssh.ECHO:          1,      // disable echoing
		cssh.TTY_OP_ISPEED: 115200, // input speed = 14.4kbaud
		cssh.TTY_OP_OSPEED: 115200, // output speed = 14.4kbaud
	}

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
		authmethod = []cssh.AuthMethod{cssh.Password(client.Password)}
	}
	Threads.SetStatus(id, START)
	conn, err := cssh.Dial("tcp", fmt.Sprintf("%s:%d", client.Ip, client.Port), &cssh.ClientConfig{
		User:            client.User,
		Auth:            authmethod,
		ClientVersion:   "SSH-2.0-MyCollector2.0",
		HostKeyCallback: cssh.InsecureIgnoreHostKey(),
		Timeout:         time.Duration(conf.Common.Timeout) * time.Second,
	})
	if err != nil {
		// panic(err)
		time.Sleep(time.Duration(Threads.GetTimeout(id)) * time.Second)
		Threads.SetStatus(id, RESTARTERR)
		log.Printf("Hostname: %s ID: %d  Error: %s", client.Hostname, id, err)
		log.Printf("Set for %d status %d", id, RESTARTERR)
		return err
	}
	defer conn.Close()
	Threads.SetStatus(id, RUN)
	Threads.SetTimeout(id, 2)

	session, err := conn.NewSession()
	handleError(err)
	defer session.Close()

	sshOut, err := session.StdoutPipe()
	handleError(err)
	sshIn, err := session.StdinPipe()
	err = session.RequestPty("vt100", 0, 0, modes)
	handleError(err)
	err = session.Shell()
	handleError(err)

	if conf.Common.Output_to_file {
		t := time.Now()
		date := fmt.Sprintf("%d%02d%02d_%02d%02d%02d", t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second())
		filename = fmt.Sprintf("%s%s_%s_%s.txt", conf.Common.Base_dir, strings.ToUpper(client.Profile), strings.ToUpper(client.Hostname), date)
		file, err = os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Printf("Host: %s Error: ", client.Hostname)
			log.Printf("Unable to create file for saving output data from ssh node: %#v\n", err)
			return err
		}
		defer file.Close()
		filenames.Add(filename)
	}

	if Threads.GetStatus(id) == FIN {
		log.Print("[SCC] Finish to work")
		return nil
	}

	ticker := time.NewTicker(time.Duration(conf.Common.Interval) * time.Second)
	// inticker := time.NewTicker(time.Duration(conf.Inbound.Timer) * time.Nanosecond)
	keepalive := time.NewTicker(time.Duration(conf.Common.Keepalive) * time.Second)

	if conf.Inbound.Enable {
		//for getting inbound commands from RabbitMQ
		wg_func.Add(1)
		go func() {
			defer wg_func.Done()
			log.Print("inbound started to work")

			type inbound_command struct {
				mutex sync.Mutex
				data  map[string]interface{}
			}
			var inbound_cmd inbound_command

			buf := make([]byte, 65535)

			in_session, err := conn.NewSession()
			handleError(err)
			defer in_session.Close()
			in_sshOut, err := in_session.StdoutPipe()
			handleError(err)
			in_sshIn, err := in_session.StdinPipe()
			err = in_session.RequestPty("vt100", 0, 0, modes)
			handleError(err)
			err = in_session.Shell()
			handleError(err)

			write(conf.Common.Dumb+"\n", in_sshIn)
			_, _ = in_sshOut.Read(buf)

			log.Printf("%d:%s:Second session was opened", id, client.Hostname)

			msgs, err := inbound.amqpChannel.Consume(client.Hostname+"_in", "", false, false, false, false, nil)
			if err != nil {
				log.Printf("%d: Failed to register a consumer, error: %s", id, err)
			}
			wg_func.Add(1)
			stopChan := make(chan bool)
			go func() {
				defer wg_func.Done()

				// msgs, err := ch.Consume(
				//                 q.Name, // queue
				//                 "",     // consumer
				//                 false,  // auto-ack
				//                 false,  // exclusive
				//                 false,  // no-local
				//                 false,  // no-wait
				//                 nil,    // args
				//         )
				//

				waitingPromptRg, _ := regexp.Compile(fmt.Sprintf("%s.*%s", client.Hostname, conf.Profiles[client.Profile].Unenable_prompt))
				waitBracket := conf.Profiles[client.Profile].Unenable_prompt

				if conf.Common.Debug >= LOW {
					log.Printf("Reset to default waiting prompt in second session: %s", waitBracket)
				}

				for d := range msgs {
					log.Printf("[NEW CMD] %s", d.Body)
					inbound_cmd.mutex.Lock()
					if err := json.Unmarshal(d.Body, &inbound_cmd.data); err != nil {
						log.Printf("%d: Error during unmarshallilng json data, error: %s", id, err)
						break
					}
					nodename := inbound_cmd.data["node"].(string)
					in_command := inbound_cmd.data["command"].(string)
					inbound_cmd.mutex.Unlock()

					if nodename == client.Hostname {
						if err := d.Ack(false); err != nil {
							log.Printf("%d: Error acknowledging message : %s", id, err)
						} else {
							if conf.Common.Debug >= LOW {
								log.Printf("%d: Acknowledged message", id)
							}
						}
						log.Printf("%d: Received command '%s' for node %s", id, in_command, nodename)
						// n, err := write_bytes(inbound_cmd["command"].(string), in_sshIn)
						err := write(in_command+"\n", in_sshIn)
						if err != nil {
							log.Printf("%d: Error send command to second session, error: %s", id, err)
							break
						}
						time.Sleep(time.Duration(conf.Common.Timeout_btw_cmd) * time.Second)

						if in_command == conf.Profiles[client.Profile].Enable_enter_command {
							waitingPromptRg, _ = regexp.Compile(fmt.Sprintf("%s.*%s", client.Hostname, conf.Profiles[client.Profile].Enable_prompt))
							waitBracket = fmt.Sprintf("%s.*%s", client.Hostname, conf.Profiles[client.Profile].Enable_prompt)
						}
						if in_command == conf.Profiles[client.Profile].Enable_exit_command {
							waitingPromptRg, _ = regexp.Compile(fmt.Sprintf("%s.*%s", client.Hostname, conf.Profiles[client.Profile].Unenable_prompt))
							waitBracket = fmt.Sprintf("%s.*%s", client.Hostname, conf.Profiles[client.Profile].Unenable_prompt)
						}

						if conf.Common.Debug >= LOW {
							log.Printf("plugin (second session) prompt: %s", waitBracket)
						}

						in_response := readSSHInput(in_sshOut, waitingPromptRg)
						json_output := map[string]string{
							"command":   in_command,
							"ip":        client.Ip,
							"nodename":  client.Hostname,
							"output":    string(in_response),
							"timestamp": fmt.Sprintf("%d", time.Now().UnixNano()),
						}
						in_json, err := json.Marshal(json_output)
						if err != nil {
							log.Printf("%d: Error receives output from second session, error: %s", id, err)
							break
						}
						publishInbound(inbound.amqpChannel, conf.Inbound.Routing_key, in_json)
					} else {
						log.Printf("%d:not my node: %s", id, nodename)
						break
					}
				}
				return
			}()
			<-stopChan
			// case data := <- channel:
			//   log.Printf("[inbound_cmd] finished")
			//   write("exit\n", in_sshIn)
			//   inticker.Stop()
			//   log.Printf("[inbound cmd] exit, %s", data)
			//   return
			//   }
			// }
			defer log.Print("[inbound cmd] inbound finished to work")
			return
		}()
	}

	// MAIN function here
	wg_func.Add(1)
	go func() {
		defer wg_func.Done()

		waitingPromptRg, _ := regexp.Compile(fmt.Sprintf("%s.*%s", client.Hostname, conf.Profiles[client.Profile].Unenable_prompt))
		waitBracket := conf.Profiles[client.Profile].Unenable_prompt

		for {
			time.Sleep(time.Duration(10000 * time.Nanosecond))
			select {
			case <-ticker.C:
				// Main commands
				for _, cmd := range commands {
					if client.Profile == "Router" {
						if cmd[:3] == "rtr" {
							command := strings.Split(cmd, ":")

							if command[1] == conf.Profiles[client.Profile].Enable_enter_command {
								waitingPromptRg, _ = regexp.Compile(fmt.Sprintf("%s.*%s", client.Hostname, conf.Profiles[client.Profile].Enable_prompt))
								waitBracket = fmt.Sprintf("%s.*%s", client.Hostname, conf.Profiles[client.Profile].Enable_prompt)
							}
							if command[1] == conf.Profiles[client.Profile].Enable_exit_command {
								waitingPromptRg, _ = regexp.Compile(fmt.Sprintf("%s.*%s", client.Hostname, conf.Profiles[client.Profile].Unenable_prompt))
								waitBracket = fmt.Sprintf("%s.*%s", client.Hostname, conf.Profiles[client.Profile].Unenable_prompt)
							}

							if conf.Common.Debug >= LOW {
								log.Printf("%s:%d:%d:%s:%s:%s", client.Hostname, id, Threads.GetStatus(id), command[0], waitBracket, command[1])
							}

							err = write(command[1]+"\n", sshIn)
							if err == io.EOF {
								ticker.Stop()
								keepalive.Stop()
								log.Printf("EoF on %s", client.Hostname)
								Threads.SetStatus(id, ERR)
								log.Printf("Set for %d status %d", id, ERR)
								return
							}
							time.Sleep(time.Duration(conf.Common.Timeout_btw_cmd) * time.Second)

							response = readSSHInput(sshOut, waitingPromptRg)
							json_output := map[string]string{
								"command":   command[1],
								"ip":        client.Ip,
								"nodename":  client.Hostname,
								"output":    string(response),
								"timestamp": fmt.Sprintf("%d", time.Now().UnixNano()),
							}
							json1, err := json.Marshal(json_output)
							if err != nil {
								log.Println("Error during marshalling of JSON object: ", err)
							}
							if conf.Common.Output_to_file {
								file.Write(response)
							}
							if plugin_key, ok := checkPlugin(command[1], plugins); ok == true {
								if plugins[plugin_key].class == "amqp" {
									publish_result := publishPlugin(plugins[plugin_key].amqpSocket, plugins[plugin_key].uniq, json1)
									if !publish_result {
										log.Printf("Re-init for plugin: %s", plugins[plugin_key].uniq)
										// init plugin and publish again
										plugins[plugin_key] = ReinitOnePluginChannel(plugins[plugin_key])
										_ = publishPlugin(plugins[plugin_key].amqpSocket, plugins[plugin_key].uniq, json1)
									}
								} else {
									plugins[plugin_key].socket.Write(json1)
								}
							}
						}
					}
				}

			case <-keepalive.C:
				write(conf.Common.Dumb+"\n", sshIn)
				// readBuffForString_constant(sshOut, client.Hostname+conf.Profiles[client.Profile].Enable_prompt)

			case data := <-channel:
				Threads.SetStatus(id, FIN)
				log.Printf("Set for id %d status %d", id, FIN)
				write("exit\n", sshIn)
				write("exit\n", sshIn)
				ticker.Stop()
				keepalive.Stop()
				log.Printf("[SCC] exit, %s", data)
				return
			}
		}
		// log.Printf("[SCC] exit, %d", id)
	}()

	wg_func.Wait()

	return nil
}
