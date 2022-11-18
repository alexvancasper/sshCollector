#!/usr/bin/python3

import pika
import json
import re
from time import sleep

def show_data(ch, method, properties, input):
        input = input.decode("utf-8")
        input = json.loads(input)
        node = input["nodename"]
        result = input["output"].split("\n")
        command = input["command"]
        timestamp = input["timestamp"]
        print(input)
        
        cmd="show chassis detail"
        raw_data = get_add_data(node, cmd)
        print(raw_data)
        get_add_data(node,"system-view")
        raw_data2  = get_add_data(node, "display interface description")
        print(raw_data2)
        get_add_data(node, "quit")

def get_add_data(node, command):
    credentials = pika.PlainCredentials("guest", "guest")
    parameters = pika.ConnectionParameters('10.90.3.228',5672, '/', credentials )
    connection = pika.BlockingConnection(parameters)
    out_channel = connection.channel()
    out_channel.exchange_declare(exchange='inbound', durable=True, exchange_type='direct', auto_delete=True)
    message = json.dumps({"node":node, "command":command})
    out_channel.basic_publish(exchange='inbound', routing_key=node+'_in', body=message)
    print(" [x] Sent %r" % message)
    print(" [*] Waiting for logs from  \"commands\". To exit press Ctrl+C")
    # getting data:
    output=''
    with connection.channel() as in_channel:
        in_result = in_channel.queue_declare(queue="commands", auto_delete=True)
        in_queue_name = in_result.method.queue
        in_channel.queue_bind(exchange="printouts", queue=in_queue_name)

        while True:
            try:
                method_frame, header_frame, body = in_channel.basic_get(queue=in_queue_name, auto_ack=False)
            except KeyboardInterrupt:
                break
            output=body
            if output is not None:
                in_channel.basic_ack(method_frame.delivery_tag)
                break
            sleep(0.1)

    connection.close()
    return output

def rabbit():
    # receive first data from nodes
    
    credentials = pika.PlainCredentials("guest", "guest")
    parameters = pika.ConnectionParameters('10.90.3.228',5672, '/', credentials )
    connection = pika.BlockingConnection(parameters)
    input_channel = connection.channel()
    input_channel.exchange_declare(exchange="printouts", durable=True, exchange_type="direct",auto_delete=True)
    in_result = input_channel.queue_declare(queue="display_admin", exclusive=False, auto_delete=True)
    in_queue_name = in_result.method.queue
    input_channel.queue_bind(exchange="printouts", queue=in_queue_name)
    print(" [ * ] Waiting for logs routing key is \"display_admin\". To exit press Ctrl+C")
    input_channel.basic_consume(queue=in_queue_name, on_message_callback=show_data, auto_ack=True)
    try:
        input_channel.start_consuming()
    except KeyboardInterrupt:
        input_channel.stop_consuming()
        print("Closing connection normally")

    input_channel.close()
    connection.close()


def main():
    rabbit()

if __name__ == '__main__':
   main()
