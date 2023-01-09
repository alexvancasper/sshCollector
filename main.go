package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/BurntSushi/toml"
)

var (
	conf       Config
	GID        int
	BuildTime  string
	GitCommit  string
	GitComment string
	version    string
)

type Config struct {
	Title    string
	Version  string
	Common   common
	Upload   Upload
	Download download
	Local    local
	Auth     authentication `toml:"authentication"`
	Devices  map[string]Client
	Profiles map[string]profiles
	Parser   map[string]parser
	Inbound  inbound_cmd `toml:"inbound_commands"`
}

type inbound_cmd struct {
	Enable      bool   `toml:"enable"`
	Socket      string `toml:"socket"`
	Queue       string `toml:"quene_name"`
	Exchange    string `toml:"exchange_name"`
	Routing_key string `toml:"routing_key"`
	Timer       uint16 `toml:"timer"`
}

type parser struct {
	Enable             bool
	Socket             string
	Uniq               string
	Key                string
	Amqp_exchange_name string `toml:"amqp_exchange_name"`
}

type authentication struct {
	One_for_all bool
	Pubkey      bool
	Pubkey_path string
	Username    string
	Password    string
}

type download struct {
	Enable    bool
	Devices   string
	Filename  string
	Dstfolder string `toml:"destination_folder"`
}

type common struct {
	Log_file        string
	Zipping         bool
	Debug           uint8
	Remove          bool `toml:"remove_zipped_files"`
	Collect         bool `toml:"collect_data"`
	Timeout         uint16
	Stay            bool `toml:"stay_on_node"`
	Interval        uint
	Keepalive       uint
	Dumb            string `toml:"dumb_command"`
	Output_to_file  bool   `toml:"save_into_file"`
	Base_dir        string `toml:"base_dir"`
	Timeout_read    uint16 `toml:"timeout_prompt_read"`
	Timeout_btw_cmd uint16 `toml:"timeout_between_commands"`
}

type Upload struct {
	Upload_on_remote bool `toml:"upload_on_remote_server"`
	Remove           bool
	Request          string `toml:"type"`
	Server           string
	Port             uint16
	Username         string
	Pubkey           bool
	Password         string
	Path             string
	Http_url         string
	Fieldname        string `toml:"Field_name"`
}

type local struct {
	Copy    bool `toml:"copy_to_local_storage"`
	Storage string
}

type profiles struct {
	Name                 string `toml:"name"`
	Unenable_prompt      string `toml:"unenable_prompt"`
	Enable_prompt        string `toml:"enable_prompt"`
	App_prompt           string `toml:"app_prompt"`
	Enable_enter_command string `toml:"enable_enter_command"`
	Enable_exit_command  string `toml:"enable_exit_command"`
	Command_file         string `toml:"command_file"`
}

type Client struct {
	Hostname string
	Ip       string
	User     string
	Password string //password or key file path
	Port     uint16
	Profile  string
}

type State struct {
	mutex   sync.Mutex
	name    string
	state   int
	timeout int
}

type States struct {
	mutex  sync.Mutex
	worker []State
}

type Output_file struct {
	mutex    sync.Mutex
	filename []string
}

const (
	LOW    uint8 = 3
	MEDIUM uint8 = 2
	HIGH   uint8 = 1

	MAX_TIMEOUT int = 3600 // timeout between attempts of reconnecting of worker in case of cssh.Dial returned FAIL. seconds

	NEW        int = 0
	RUN        int = 1
	START      int = 2
	RESTARTERR int = 3
	ERR        int = 8
	FIN        int = 9
)

func (rab *States) AddWorker(device string) int {
	new := State{name: device, state: NEW, timeout: 2}
	rab.mutex.Lock()
	rab.worker = append(rab.worker, new)
	rab.mutex.Unlock()
	return len(rab.worker) - 1
}
func (rab *States) GetLength() int {
	rab.mutex.Lock()
	lenght := len(rab.worker)
	rab.mutex.Unlock()
	return lenght
}
func (rab *States) GetId(device string) int {
	rab.mutex.Lock()
	var i int = -1
	for idx, worker := range rab.worker {
		// fmt.Println(idx)
		i = idx
		if worker.name == device {
			break
		}

	}
	rab.mutex.Unlock()
	return i
}
func (rab *States) GetStatus(id int) int {
	rab.mutex.Lock()
	rab.worker[id].mutex.Lock()
	status := rab.worker[id].state
	rab.worker[id].mutex.Unlock()
	rab.mutex.Unlock()
	return status
}
func (rab *States) GetByStatus(state int) int {
	rab.mutex.Lock()
	var k int = 0
	for _, worker := range rab.worker {
		if worker.state == state {
			k += 1
		}
	}
	rab.mutex.Unlock()
	log.Printf("State is %d => %d", state, k)
	return k
}
func (rab *States) SetStatus(id int, status int) error {
	rab.mutex.Lock()
	total := len(rab.worker)
	// rab.mutex.Unlock()
	if total > id {
		rab.worker[id].mutex.Lock()
		rab.worker[id].state = status
		rab.worker[id].mutex.Unlock()
	} else {
		rab.mutex.Unlock()
		return errors.New("There is no index " + string(id))
	}
	rab.mutex.Unlock()
	return nil
}
func (rab *States) GetTimeout(id int) int {
	rab.mutex.Lock()
	rab.worker[id].mutex.Lock()
	timeout := rab.worker[id].timeout
	rab.worker[id].mutex.Unlock()
	rab.mutex.Unlock()
	return timeout
}
func (rab *States) SetTimeout(id int, timeout int) {
	rab.worker[id].mutex.Lock()
	rab.worker[id].timeout = timeout
	rab.worker[id].mutex.Unlock()
}

func (rab *States) SetName(id int, name string) {
	rab.worker[id].mutex.Lock()
	rab.worker[id].name = name
	rab.worker[id].mutex.Unlock()
}
func (rab *States) GetName(id int) string {
	rab.worker[id].mutex.Lock()
	name := rab.worker[id].name
	rab.worker[id].mutex.Unlock()
	return name
}

func (output *Output_file) Add(filename string) {
	output.mutex.Lock()
	if !stringInSlice(filename, output.filename) {
		output.filename = append(output.filename, filename)
	}
	output.mutex.Unlock()
}

func (output *Output_file) Length() int {
	output.mutex.Lock()
	result := len(output.filename)
	output.mutex.Unlock()
	return result
}
func (output *Output_file) Contains() []string {
	return output.filename
}

func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

func ParseConfig(conf Config) {
	// log.Printf("%#v\n",conf)
	log.Print("About:")
	log.Printf("Version: %s\n", version)
	log.Printf("Build Time: %s\n", BuildTime)
	log.Printf("GIT Commit: %s\n", GitCommit)
	log.Printf("GIT Commit: %s\n", GitComment)
	log.Print("General section")
	log.Printf("-Title: %s\n", conf.Title)
	log.Printf("-Version: %s\n", conf.Version)
	log.Print("--Common:")
	log.Printf("---Log file: %s\n", conf.Common.Log_file)
	log.Printf("---Destination folder: %s\n", conf.Common.Base_dir)
	log.Printf("---Zipping: %t\n", conf.Common.Zipping)
	log.Printf("---Debug level: %d\n", conf.Common.Debug)
	log.Printf("---Remove zipped files: %t\n", conf.Common.Remove)
	log.Printf("---Collect Data: %t\n", conf.Common.Collect)
	log.Printf("---Timeout: %d\n", conf.Common.Timeout)
	log.Printf("---Dump ommand: %s\n", conf.Common.Dumb)
	log.Print("--Inbound commands:")
	if conf.Inbound.Enable {
		log.Printf("---Enable: %t\n", conf.Inbound.Enable)
		log.Printf("---Socket: %s\n", conf.Inbound.Socket)
		log.Printf("---Queue: %s\n", conf.Inbound.Queue)
		log.Printf("---Exchange: %s\n", conf.Inbound.Exchange)
		log.Printf("---Routing key: %s\n", conf.Inbound.Routing_key)
		log.Printf("---Timer: %d\n", conf.Inbound.Timer)
	} else {
		log.Printf("---Enable: %t\n", conf.Inbound.Enable)
	}
	log.Print("--Upload:")
	log.Printf("---Upload_on_remote: %t\n", conf.Upload.Upload_on_remote)
	log.Printf("---Remove: %t\n", conf.Upload.Remove)
	log.Printf("---Type: %s\n", conf.Upload.Request)
	if strings.ToLower(conf.Upload.Request) == "sftp" {
		log.Printf("---Server: %s\n", conf.Upload.Server)
		log.Printf("---Port: %d\n", conf.Upload.Port)
		log.Printf("---Username: %s\n", conf.Upload.Username)
		if conf.Upload.Pubkey {
			log.Printf("---Public RSA key: %t\n", conf.Upload.Pubkey)
		} else {
			log.Printf("---Password(base64 decoded): %s\n", conf.Upload.Password)
		}
		log.Printf("---Path: %s\n", conf.Upload.Path)
	}
	if strings.ToLower(conf.Upload.Request) == "http" {
		log.Printf("---HTTP URL: %s\n", conf.Upload.Http_url)
		log.Printf("---HTTP form field: %s\n", conf.Upload.Fieldname)
	}
	log.Print("--Download:")
	log.Printf("---Enable: %t\n", conf.Download.Enable)
	log.Printf("---Devices: %s\n", conf.Download.Devices)
	log.Printf("---File path and name: %s\n", conf.Download.Filename)
	log.Print("--Local:")
	log.Printf("---Copy Y/N: %t\n", conf.Local.Copy)
	log.Printf("---Storage: %s\n", conf.Local.Storage)
	log.Print("--Device profiles:")
	log.Printf("---Loaded device profiles: %d\n", len(conf.Profiles))
	for idx, _ := range conf.Profiles {
		log.Printf("---Name: %s\n", conf.Profiles[idx].Name)
		log.Printf("----Enable enter command: %s\n", conf.Profiles[idx].Enable_enter_command)
		log.Printf("----Enable exit command: %s\n", conf.Profiles[idx].Enable_exit_command)
		log.Printf("----Not enable prompt: %s\n", conf.Profiles[idx].Unenable_prompt)
		log.Printf("----Enable prompt: %s\n", conf.Profiles[idx].Enable_prompt)
		log.Printf("----Application prompt: %s\n", conf.Profiles[idx].App_prompt)
		log.Printf("----Command file: %s\n", conf.Profiles[idx].Command_file)
	}
	log.Print("--Authentication:")
	log.Printf("---One_for_all: %t\n", conf.Auth.One_for_all)
	log.Printf("---Pubkey: %t\n", conf.Auth.Pubkey)
	log.Printf("---pubkey_path: %s\n", conf.Auth.Pubkey_path)
	if conf.Auth.One_for_all {
		log.Printf("---Username: %s\n", conf.Auth.Username)
		log.Printf("---Password: %s\n", strings.Repeat("*", len(conf.Auth.Password)))
	}
	log.Print("--Devices:")
	log.Printf("---Loaded devices: %d\n", len(conf.Devices))
}

func main() {
	// ParseConfig(conf)
	conf_file := "./config.tml"
	if len(os.Args) == 1 {
		if _, err := os.Stat(conf_file); err == nil {
			if _, err := toml.DecodeFile(conf_file, &conf); err != nil {
				fmt.Fprintf(os.Stderr, "The config [%s] file is broken\n", conf_file)
				log.Print(err)
				flag.PrintDefaults()
				os.Exit(1)
			}
		} else {
			// path/to/whatever does *not* exist
			fmt.Fprintf(os.Stderr, "In the default place, the config file does not exists [%s]\n", conf_file)
			fmt.Fprintf(os.Stderr, "Usage %s [config_file]\n", os.Args[0])
			flag.PrintDefaults()
			os.Exit(1)
		}
	}

	if len(os.Args) == 2 {
		if _, err := toml.DecodeFile(os.Args[1], &conf); err != nil {
			fmt.Fprintf(os.Stderr, "The config [%s] file is broken\n", conf_file)
			log.Print(err)
			flag.PrintDefaults()
			os.Exit(1)
		}
	}
	if len(conf.Common.Base_dir) > 0 {
		if conf.Common.Base_dir[len(conf.Common.Base_dir)-1:] != "/" {
			conf.Common.Base_dir += "/"
		}
	} else {
		log.Print("check config in base_dir")
	}

	f, err := os.OpenFile(conf.Common.Log_file, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("Error opening file: %v", err)
	}
	defer f.Close()
	log.SetOutput(f)

	var common_commands map[string][]string
	var archive_file string

	common_commands = make(map[string][]string, len(conf.Profiles))
	for _, profile := range conf.Profiles {
		commands, _ := read_command_file(profile.Command_file)
		common_commands[profile.Name] = commands

	}

	if conf.Common.Debug >= MEDIUM {
		ParseConfig(conf)
		log.Print("--Commands:")
		for profileName, _ := range common_commands {
			log.Printf("--- Profile name: %s", profileName)
			for idx, command := range common_commands[profileName] {
				log.Printf("---- %d. %s", idx, command)
			}
		}

		log.Print("--Processed devices:")
		for idx, _ := range conf.Devices {
			log.Printf("---Hostname: %s\n", conf.Devices[idx].Hostname)
			log.Printf("---IP:\t %s\n", conf.Devices[idx].Ip)
			log.Printf("---Port:\t %d\n", conf.Devices[idx].Port)
			log.Printf("---Profile:\t %s\n", conf.Devices[idx].Profile)
			if !conf.Auth.One_for_all {
				log.Printf("---User:\t %s\n", conf.Devices[idx].User)
				log.Printf("---Password: %s\n", strings.Repeat("*", len(conf.Devices[idx].Password)))
			}
			log.Printf("------\n")
		}
	}
	if conf.Common.Collect {
		var wg sync.WaitGroup
		var plugins map[string]Plugin
		var inbound Inbound
		out_file := &Output_file{}
		Threads := &States{}

		if conf.Common.Stay {
			c := make(chan os.Signal)
			signal.Notify(c, os.Interrupt, syscall.SIGTERM)
			total_devices := len(conf.Devices)
			chans := make([]chan string, total_devices)
			for i := range chans {
				chans[i] = make(chan string)
			}

			go func() {
				// Close function for all goroutins...
				defer log.Print("[MONITOR] Finished")
				for {
					time.Sleep(time.Duration(10000000 * time.Nanosecond))
					<-c
					for j := 0; j < Threads.GetLength(); j += 1 {
						if Threads.GetStatus(j) == RUN {
							chans[j] <- fmt.Sprintf("received SIGTERM, bond number [%d] immediately stop, my Lord!", j)
						}
						log.Printf("Send to id: %d signal: %d", j, FIN)
						Threads.SetStatus(j, FIN)
					}
					log.Print("Cleanup")
					total := Threads.GetLength()
					current := Threads.GetByStatus(FIN)
					if current == total {
						break
					}
				}
				return
			}()

			if conf.Parser["global"].Enable {
				plugins = initPlugin()

				defer closePlugin(plugins)
			}
			if conf.Inbound.Enable {
				// inbound = initInbound()
				inbound = initInboundPerNode()
			}

			wg.Add(1)
			go func(wg *sync.WaitGroup) {
				defer wg.Done()
				defer log.Printf("[MAIN] STOP Working function")
				var waitg sync.WaitGroup
				finish := 0
				for {
					time.Sleep(time.Duration(100000000 * time.Nanosecond))
					if finish >= Threads.GetLength() && Threads.GetLength() != 0 {
						break
					}
					if Threads.GetLength() == 0 {
						for device := range conf.Devices {
							Threads.AddWorker(device)
						}
						for device := range conf.Devices {
							time.Sleep(time.Duration(Random()) * time.Nanosecond)
							id := Threads.GetId(device)
							waitg.Add(1)
							go ssh_collector_constant(conf.Devices[device], common_commands, &waitg,
								out_file, chans[id], Threads, id, plugins, inbound)
						}
					}
					for i := 0; i < Threads.GetLength(); i += 1 {
						switch Threads.GetStatus(i) {
						case FIN:
							finish += 1
						case RUN:
							continue
						case START:
							continue
						case RESTARTERR:
							timeout := Threads.GetTimeout(i)
							if timeout < MAX_TIMEOUT {
								timeout = int(float64(timeout) * math.Phi)
								if timeout > MAX_TIMEOUT {
									timeout = MAX_TIMEOUT
								}
							}
							Threads.SetTimeout(i, timeout)
							if conf.Common.Debug >= LOW {
								log.Printf("New Restart timeout for ID %d is %d sec", i, timeout)
							}
							fallthrough
						case ERR:
							device := Threads.GetName(i)
							if conf.Common.Debug >= MEDIUM {
								log.Printf("Restart ID: %d  Host: %s", i, conf.Devices[device].Hostname)
							}
							waitg.Add(1)
							go ssh_collector_constant(conf.Devices[device], common_commands, &waitg,
								out_file, chans[i], Threads, i, plugins, inbound)
						}
						time.Sleep(1 * time.Second)
					}
				}
				// waitg.Wait()
			}(&wg)

			wg.Wait()
			fmt.Println("[Stay] exit :)")

			if conf.Inbound.Enable {
				defer closeInbound(inbound)
			}

		} else {
			for idx, _ := range conf.Devices {
				wg.Add(1)
				go ssh_collector(conf.Devices[idx], common_commands, &wg, out_file)
			}
			wg.Wait()
		}

		log.Print("Collector finished")

		if conf.Common.Zipping {
			if conf.Common.Debug >= MEDIUM {
				log.Print("Start to archivhing files")
			}
			if conf.Common.Debug >= LOW {
				log.Printf("Total files: %d", out_file.Length())
				log.Printf("File names: %v\n", out_file.Contains())
			}

			if out_file.Length() == 0 {
				log.Print("There no files for archiving")
			} else {
				archive_file, _ = zipping(out_file.Contains())
				log.Printf("Archive filename: %s\n", archive_file)
				if conf.Common.Debug >= MEDIUM {
					log.Print("Zipping done")
				}

				if conf.Local.Copy {
					if conf.Local.Storage[len(conf.Local.Storage)-1:] != "/" {
						conf.Local.Storage += "/"
					}
					destinationFile := conf.Local.Storage + filepath.Base(archive_file)
					if conf.Common.Debug >= MEDIUM {
						log.Printf("Start to moving the file to %s", destinationFile)
					}
					nBytes, err := copy(archive_file, destinationFile)
					if err != nil {
						log.Printf("The copy operation failed %q\n", err)
					} else {
						log.Printf("Copied %d bytes!\n", nBytes)
					}
				}
			}
		}

	}

	if conf.Upload.Upload_on_remote {
		var result bool = false

		if conf.Common.Debug >= MEDIUM {
			log.Print("Starting to upload")
		}

		if _, err := os.Stat(archive_file); os.IsNotExist(err) {
			log.Print("Source file for uploading does not exists")
			log.Fatal(err)
		}

		if strings.ToLower(conf.Upload.Request) == "sftp" {
			result = upload_on_remote_scp(archive_file, &conf.Upload)

		}
		if strings.ToLower(conf.Upload.Request) == "http" {
			result = postFile(archive_file, conf.Upload.Http_url, conf.Upload.Fieldname)
		}

		if result {
			log.Print("File was uploaded successfully")
			if conf.Upload.Remove {
				err = os.Remove(archive_file)
				handleError(err)
				if conf.Common.Debug >= MEDIUM {
					log.Printf("Removed file %s", archive_file)
				}
			}
		} else {
			log.Print("File was not uploaded")
		}
	}

	if conf.Download.Enable {
		if conf.Common.Debug >= MEDIUM {
			log.Print("Download file from node. Start")
		}

		var delimeter string = ","
		var download_result bool
		devices := strings.Split(conf.Download.Devices, delimeter)
		if _, err := os.Stat(conf.Download.Dstfolder); os.IsNotExist(err) {
			log.Printf("Destination file does not exist: %s\n", conf.Download.Dstfolder)
		} else {
			for client := range conf.Devices {
				for num := range devices {
					if conf.Devices[client].Profile == devices[num] {
						download_result = download_from_remote_scp(conf.Download.Filename, conf.Download.Dstfolder, conf.Devices[client])
						if conf.Common.Debug >= LOW {
							if download_result {
								log.Printf("[Download] Hostname %s file downloaded", conf.Devices[client].Hostname)
							} else {
								log.Printf("[Download] Hostname %s file was not downloaded", conf.Devices[client].Hostname)
							}
						}
					}
				}
			}
		}
	}

} // main
