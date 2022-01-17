package main

import (
  // "fmt"
  // cssh "golang.org/x/crypto/ssh"
  // agent "golang.org/x/crypto/ssh/agent"
  // "os"
  // "io"
  "net"
  "log"
  "strings"
  "regexp"
  "golang.org/x/crypto/blake2b"
  "encoding/hex"
  "github.com/streadway/amqp"
  "reflect"
)

type Plugin struct {
  uniq string
  str_socket string
  socket net.Conn
  amqpSocket *amqp.Channel
  amqpConn *amqp.Connection
  key string
  class string
}

type prevAMQP struct {
  amqpSocket *amqp.Channel
  amqpConn *amqp.Connection
  strSocket string
  uniq string
  key string
}

type Inbound struct {
  amqpChannel *amqp.Channel
  amqpConn *amqp.Connection
  // messages
}

func GetHash(text string) string {
    hasher, _ := blake2b.New256([]byte(text))
    hasher.Write([]byte(text))
    return hex.EncodeToString(hasher.Sum(nil))
}

func initPlugin() map[string]Plugin {
  var plugin Plugin
  var prevAConn prevAMQP
  plugins := make(map[string]Plugin)
  log.Printf("Total plugins: %d\n",len(conf.Parser)-1)
  if conf.Common.Debug >= MEDIUM {
    log.Print("Initialization of plugins...")
    log.Printf("AMQP broker exchange point: %s\n", conf.Parser["global"].Amqp_exchange_name)
    for parser := range conf.Parser {
      if parser=="global" {continue}
      if !conf.Parser[parser].Enable {continue}
      log.Printf("Plugin name: %s socket: %s uniq: %s",parser, conf.Parser[parser].Socket, conf.Parser[parser].Uniq)
      }
  }
  for parser := range conf.Parser {
    if parser=="global" {continue}
    if !conf.Parser[parser].Enable {continue}
    divided_socket:=strings.SplitN(conf.Parser[parser].Socket, ":",2)
    if divided_socket[0]==prevAConn.strSocket {
      plugin.class = "amqp"
      plugin.amqpSocket = prevAConn.amqpSocket
      plugin.amqpConn = prevAConn.amqpConn
      plugin.uniq = conf.Parser[parser].Uniq
      plugin.key = conf.Parser[parser].Key
      plugins[GetHash(plugin.uniq)] = plugin
      continue
    }
    if divided_socket[0]=="amqp"{
      cfg := amqp.Config{
        Properties: amqp.Table{
          "connection_name": conf.Title +" "+ conf.Version,
        },
      }
      conn, err := amqp.DialConfig(conf.Parser[parser].Socket, cfg)
      if err != nil {
        log.Printf("ALERT:Cannot connect to AMQP broker error: %s", err)
        continue
      }
      // defer conn.Close()
      ch,err := conn.Channel()
      if err != nil {
        log.Printf("ALERT:Failed to open AMQP channel: %s", err)
        continue
      }
      // defer ch.Close()
      err = ch.ExchangeDeclare(
        conf.Parser["global"].Amqp_exchange_name, // name
        "direct", // type
        true,     // durable
        true,    // auto-deleted
        false,    // internal
        false,    // no-wait
        nil,      // arguments
      )
      if err != nil {
        log.Printf("ALERT:Failed to declare exahange point: %s", err)
        continue
      }
//---------------
      q, err := ch.QueueDeclare(
              conf.Parser[parser].Uniq,    // name
              false, // durable
              true, // delete when unused
              false,  // exclusive
              false, // no-wait
              nil,   // arguments
      )

      if err != nil {
        log.Printf("Failed to declare a queue, error: %s", err)
      }
      err = ch.QueueBind(
              q.Name, // queue name
              "", // routing key
              conf.Parser["global"].Amqp_exchange_name, // exchange
              false,
              nil,
      )
//---------------

      if conf.Common.Debug >= MEDIUM {
        log.Printf("AMQP connected to: %s",conf.Parser[parser].Socket)
      }
      //Save for checking repeated AMQP brokers
      prevAConn.strSocket = divided_socket[0]
      prevAConn.amqpSocket = ch
      prevAConn.amqpConn = conn
      //Save for new AMQP connections
      plugin.class = "amqp"
      plugin.str_socket = conf.Parser[parser].Socket
      plugin.amqpSocket = ch
      plugin.amqpConn = conn
      plugin.uniq = conf.Parser[parser].Uniq
      plugin.key = conf.Parser[parser].Key
      plugins[GetHash(plugin.uniq)] = plugin
    } else {
      conn, err := net.Dial(divided_socket[0], divided_socket[1])
      if err != nil {
        log.Printf("Error happened %s\n", err)
        continue
      }
      plugin.class = "net"
      plugin.socket = conn
      plugin.uniq = conf.Parser[parser].Uniq
      plugin.key = conf.Parser[parser].Key
      plugins[GetHash(plugin.uniq)] = plugin
      // plugins.key = append(plugins.key, conf.Parser[parser].key)
    }
  }
  return plugins
}

func initOnePlugin(plugin Plugin) Plugin {

  closeOnePlugin(plugin)

  if plugin.amqpConn.IsClosed() {
    var newInitPlugin Plugin

    log.Print("initOnePlugin: connection is dead")
    cfg := amqp.Config{
      Properties: amqp.Table{
        "connection_name": "initOnePlugin dead",
      },
    }
    conn, err := amqp.DialConfig(plugin.str_socket, cfg)
    if err != nil {
      log.Printf("ALERT:Cannot connect to AMQP broker error: %s", err)
    }
    ch,err := conn.Channel()
    if err != nil {
      log.Printf("ALERT:Failed to open AMQP channel: %s", err)
    }
    err = ch.ExchangeDeclare(
      conf.Parser["global"].Amqp_exchange_name, // name
      "direct", // type
      true,     // durable
      true,    // auto-deleted
      false,    // internal
      false,    // no-wait
      nil,      // arguments
    )
    if err != nil {
      log.Printf("ALERT:Failed to declare exahange point: %s", err)
    }
  //---------------
    q, err := ch.QueueDeclare(
            plugin.uniq,    // name
            false, // durable
            true, // delete when unused
            false,  // exclusive
            false, // no-wait
            nil,   // arguments
    )

    if err != nil {
      log.Printf("Failed to declare a queue, error: %s", err)
    }
    err = ch.QueueBind(
            q.Name, // queue name
            "", // routing key
            conf.Parser["global"].Amqp_exchange_name, // exchange
            false,
            nil,
    )
  //---------------

    if conf.Common.Debug >= MEDIUM {
      log.Printf("AMQP connected to: %s",plugin.str_socket)
    }
    //Save for new AMQP connections
    newInitPlugin.class = "amqp"
    newInitPlugin.str_socket = plugin.str_socket
    newInitPlugin.amqpSocket = ch
    newInitPlugin.amqpConn = conn
    newInitPlugin.uniq = plugin.uniq
    newInitPlugin.key = plugin.key

    return newInitPlugin
  }
  log.Print("initOnePlugin: connection is alive")
  return plugin
}

//ReinitOnePluginChannel Reinit of AMQP channel if it is closed.
func ReinitOnePluginChannel(plugin Plugin) Plugin {

  plugin.amqpSocket.Close()

  ch,err := plugin.amqpConn.Channel()
  if err != nil {
    log.Printf("ALERT:Failed to open AMQP channel: %s", err)
  }
  err = ch.ExchangeDeclare(
    conf.Parser["global"].Amqp_exchange_name, // name
    "direct", // type
    true,     // durable
    true,    // auto-deleted
    false,    // internal
    false,    // no-wait
    nil,      // arguments
  )
  if err != nil {
    log.Printf("ALERT:Failed to declare exahange point: %s", err)
  }
//---------------
  q, err := ch.QueueDeclare(
          plugin.uniq,    // name
          false, // durable
          true, // delete when unused
          false,  // exclusive
          false, // no-wait
          nil,   // arguments
  )

  if err != nil {
    log.Printf("Failed to declare a queue, error: %s", err)
  }
  err = ch.QueueBind(
          q.Name, // queue name
          "", // routing key
          conf.Parser["global"].Amqp_exchange_name, // exchange
          false,
          nil,
  )
//---------------

  if conf.Common.Debug >= MEDIUM {
    log.Printf("AMQP connected to: %s",plugin.str_socket)
  }
  //Save for new AMQP connections
  plugin.amqpSocket = ch

  return plugin
}

func publishPlugin(ch *amqp.Channel, routing_key string, body []byte ) bool {
  // body := body
  err := ch.Publish(
    conf.Parser["global"].Amqp_exchange_name,
    routing_key, //q.Name,
    false,
    false,
    amqp.Publishing{
      ContentType: "text/plain",
      Body: body,
    })
    if err != nil {
      log.Printf("ALERT: publishPlugin: Error happened %s\n", err)
      return false
    }
    return true
}

func closePlugin(plugins map[string]Plugin) {
  keys := reflect.ValueOf(plugins).MapKeys()
  for _, value := range keys {
      if plugins[value.Interface().(string)].class=="amqp"{
        plugins[value.Interface().(string)].amqpSocket.Close()
        plugins[value.Interface().(string)].amqpConn.Close()
      }
  }
}

func closeOnePlugin(plugin Plugin) {
    if plugin.class=="amqp"{
      plugin.amqpSocket.Close()
      plugin.amqpConn.Close()
    }
}

func checkPlugin(command string, plugins map[string]Plugin) (string, bool) {
    var cmd_pattern *regexp.Regexp
    // log.Printf("Lenght of Plugins: %d", len(plugins))
    for plugin := range plugins {
      // log.Printf("Key of Plugins: %s",plugin)
      cmd_pattern = regexp.MustCompile(plugins[plugin].key)
      result := cmd_pattern.FindString(command);
      // log.Printf("Key: %s <-> Command: %s <-> Uniq: %s => Result: %s" ,plugins[plugin].key, command, plugins[plugin].uniq, result)
      if len(result)!=0 { return plugin, true }
    }
    return "",false
  }

func publishInbound(ch *amqp.Channel, routing_key string, body []byte ) {
  // body := body
  err := ch.Publish(
    conf.Parser["global"].Amqp_exchange_name,
    routing_key, //q.Name,
    false,
    false,
    amqp.Publishing{
      ContentType: "text/plain",
      Body: body,
    })
    if err != nil {
      log.Printf("ALERT: publishInbound: Error happened %s\n", err)
    }
}

func closeInbound(ch Inbound){
  ch.amqpChannel.Close()
  ch.amqpConn.Close()
}



func initInboundPerNode() Inbound {
  var output Inbound
  var err error
  cfg := amqp.Config{
    Properties: amqp.Table{
      "connection_name": conf.Title +" "+ conf.Version+" inbound",
    },
  }
  output.amqpConn, err = amqp.DialConfig(conf.Inbound.Socket, cfg)
  if err != nil {
    log.Printf("Failed to connect to RabbitMQ, error: %s", err)
  }
  output.amqpChannel, err = output.amqpConn.Channel()
  if err != nil {
    log.Printf("Failed to open a channel, error: %s", err)
  }
  err = output.amqpChannel.ExchangeDeclare(
          conf.Inbound.Exchange,   // name
          "direct", // type
          true,     // durable
          true,    // auto-deleted
          false,    // internal
          false,    // no-wait
          nil,      // arguments
  )
  if err != nil {
    log.Printf("Failed to declare an exchange, error: %s", err)
  }

  for idx, _ := range conf.Devices {
    q, err := output.amqpChannel.QueueDeclare(
            conf.Devices[idx].Hostname+"_in",    // name
            false, // durable
            true, // delete when unused
            false,  // exclusive
            false, // no-wait
            nil,   // arguments
    )
    if err != nil {
      log.Printf("Failed to declare a queue, error: %s", err)
    }
    err = output.amqpChannel.QueueBind(
            q.Name, // queue name
            conf.Devices[idx].Hostname+"_in", // routing key
            conf.Inbound.Exchange, // exchange
            false,
            nil,
    )

    if err != nil {
      log.Printf("Failed to bind a queue, error: %s", err)
    }

    if conf.Common.Debug >= MEDIUM {
      log.Printf("AMQP INBOUND connected to: %s",conf.Inbound.Socket)
    }

  } // for loop
  return output
}
