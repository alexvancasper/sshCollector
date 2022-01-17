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

        SLOT = "RPSW\d|ALSW\d|SW\d|\d+"
        LC = "rpsw|alsw|alswt|rpsw2|alsw2|rpsw-v2|sw|sw2|ge-40-port|10ge-10-port|1-10ge-20-port|1-10ge-20-4-port|40-100ge-2-port|ssc1|ssc1-v2|ssc2-a|ssc3|1-100ge-24-2-port|none|n\/a"
        BAD_IS_STATES = "IS-Degraded|OOS-Booting|OOS-Fault|OOS-Init"
        NORMAL_IS_STATES = "IS|OOS-Deactivating|OOS-Diag|OOS-NotActivated|OOS-Shutdown|OOS-Unassigned|n\/a"
        GOOD_IS_STATES = "IS"
        OPER_STATES = GOOD_IS_STATES
        chassis_pattern = re.compile("(" + SLOT + ")\s:\s(" + LC + ")\s+(" + LC + ")\s+(" + OPER_STATES + ")")
        vogon=[]
        caldera=[]
        ppa3lp=[]
        for line in result:
            value = chassis_pattern.search(line)
            if value:
                slot_value = value.group(1)
                conf_type_value = value.group(2)
                instal_type_value = value.group(3)
                is_state = value.group(4)
                if slot_value.strip().isdigit() and conf_type_value==instal_type_value:
                    if instal_type_value == "10ge-10-port":
                        vogon.append(slot_value)
                    if instal_type_value == "1-10ge-20-port":
                        caldera.append(slot_value)
                    if instal_type_value == "1-10ge-20-4-port":
                        ppa3lp.append(slot_value)

        for slot in ppa3lp:
            cmd = "show card {} ppa nat allocation".format(slot)
            translation_data = nat_allocation(node, cmd)
            print(translation_data)




def show_chassis(in_lines, nodename):
    SLOT = "RPSW\d|ALSW\d|SW\d|\d+"
    LC = "rpsw|alsw|alswt|rpsw2|alsw2|rpsw-v2|sw|sw2|ge-40-port|10ge-10-port|1-10ge-20-port|1-10ge-20-4-port|40-100ge-2-port|ssc1|ssc1-v2|ssc2-a|ssc3|1-100ge-24-2-port|none|n\/a"
    BAD_IS_STATES = "IS-Degraded|OOS-Booting|OOS-Fault|OOS-Init"
    NORMAL_IS_STATES = "IS|OOS-Deactivating|OOS-Diag|OOS-NotActivated|OOS-Shutdown|OOS-Unassigned|n\/a"
    OPER_STATES = BAD_IS_STATES
    chassis_pattern = re.compile(
        "(" + SLOT + ")\s:\s(" + LC + ")\s+(" + LC + ")\s+(" + OPER_STATES + ")")
    report = '#cardstatus\n'
    report_check = ''
    for line in in_lines:
        value = chassis_pattern.search(line)
        if value:
            slot_value = value.group(1)
            instal_type_value = value.group(3)
            is_state = value.group(4)
            report += "Nodename:{node} Slot:{slot} Card:{card} has bad state:{state}\n".format(
                node=nodename, card=instal_type_value, slot=slot_value, state=is_state)
            report_check += "{node}{slot}{card}{state}\n".format(
                node=nodename, card=instal_type_value, slot=slot_value, state=is_state)



def nat_allocation(node, cmd):
    raw_data = get_add_data(node, cmd)
    parsed_data = show_card_nat_allocation(raw_data)
    return parsed_data

def get_add_data(node, command):
    # send data to the node for getting additional data
    credentials = pika.PlainCredentials('guest', 'guest')
    connection = pika.BlockingConnection(pika.ConnectionParameters(host='localhost', credentials=credentials))
    out_channel = connection.channel()
    out_channel.exchange_declare(exchange='inbound', durable=True, exchange_type='direct', auto_delete=True)
    message = json.dumps({"node":node, "command":command})
    out_channel.basic_publish(exchange='inbound', routing_key=node+'_in', body=message)
    print(" [x] Sent %r" % message)

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

def show_card_nat_allocation(in_lines):
    cardnum_pattern = re.compile(r"Slot\s(\d)\,\sIngress:")
    used_pattern = re.compile(r"used\scount\s+:\s(\d+)")
    unassigned_pattern = re.compile(r"unassigned\scount\s+:\s(\d+)")
    free_pattern = re.compile(r"free\scount\s+:\s(\d+)")
    pfe_pattern = re.compile(r"PFE\scomplex\s(\d):")

    in_lines = json.loads(in_lines)
    result = in_lines["output"].split("\n")

    hostname = in_lines["nodename"]
    command = ""
    if 'command' in in_lines.keys():
       command = in_lines["command"]
    else:
       print("no data")
       return

    cardnum = ''
    last_pfe = '0'
    pfe=''

    output = []
    for line in result:
        card_value = cardnum_pattern.search(line)
        if card_value:
            cardnum = card_value.group(1)

        pfe_value = pfe_pattern.search(line)
        if pfe_value:
            pfe = pfe_value.group(1)

            if len(last_pfe)>0 and last_pfe != pfe:
                output.append({"card":cardnum, "pfe":last_pfe, "used":used, "unassign":unassigned, "free":free})
                last_pfe=pfe

        used_value = used_pattern.search(line)
        if used_value:
            used=used_value.group(1)
        unassigned_value = unassigned_pattern.search(line)
        if unassigned_value:
            unassigned = unassigned_value.group(1)
        free_value = free_pattern.search(line)
        if free_value:
            free = free_value.group(1)

    if len(pfe)>0 and len(used)>0 and len(unassigned)>0 and len(free)>0 and len(cardnum):
        output.append({"card":cardnum, "pfe":pfe, "used":used, "unassign":unassigned, "free":free})
    re.purge()
    return output



def rabbit():
    # receive first data from nodes
    connection = pika.BlockingConnection(pika.ConnectionParameters(host='localhost'))
    input_channel = connection.channel()
    input_channel.exchange_declare(exchange="printouts", durable=True, exchange_type="direct",auto_delete=True)
    in_result = input_channel.queue_declare(queue="link_group_detail", exclusive=False)
    in_queue_name = in_result.method.queue
    input_channel.queue_bind(exchange="printouts", queue=in_queue_name)
    print(" [ * ] Waiting for logs routing key is \"link_group_detail\". To exit press Ctrl+C")
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
