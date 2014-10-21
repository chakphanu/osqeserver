package main

import (
	"bytes"
	"container/list"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"strconv"
	"time"
)

const SERVER_NAME = "_SERVER_"
const ROOM_ALL = "_ALL_"
const MAX_ROOM_LENGHT = 10
const MAX_NAME_LENGHT = 10
const MAX_MESSAGE_LENGHT = 100
const MAX_CLIENT_PER_ROOM = 100
const MAX_PASSWORD_LENGHT = 20

var (
	Trace   *log.Logger
	Info    *log.Logger
	Warning *log.Logger
	Error   *log.Logger
)

type DataSend struct {
	Name   string
	Room   string
	Data   string
	Lock   bool
	Unlock bool
}

type Client struct {
	Name       string
	Room       string
	Message    string
	Password   string
	Incoming   chan DataSend
	Outgoing   chan DataSend
	Conn       net.Conn
	Quit       chan bool
	ClientList *list.List
	MuteList   map[string]struct{}
	roomRead   chan TRoomRead
	roomAdd    chan TRoomAdd
	roomRemove chan TRoomRemove
}

type Room struct {
	Name       string
	ClientList *list.List
}

type TRoomRead struct {
	RoomName   string
	ClientList chan *list.List
}

type TRoomAdd struct {
	RoomName string
	client   *Client
}

type TRoomRemove struct {
	client *Client
}

type Join struct {
	Name     []byte
	Room     []byte
	Message  []byte
	Password []byte
}

func Init(
	traceHandle io.Writer,
	infoHandle io.Writer,
	warningHandle io.Writer,
	errorHandle io.Writer) {

	Trace = log.New(traceHandle,
		"TRACE: ",
		log.Ldate|log.Ltime|log.Lshortfile)

	Info = log.New(infoHandle,
		"INFO: ",
		log.Ldate|log.Ltime|log.Lshortfile)

	Warning = log.New(warningHandle,
		"WARNING: ",
		log.Ldate|log.Ltime|log.Lshortfile)

	Error = log.New(errorHandle,
		"ERROR: ",
		log.Ldate|log.Ltime|log.Lshortfile)
}

func main() {
	Init(ioutil.Discard, os.Stdout, os.Stdout, os.Stderr)
	//Init(os.Stdout, os.Stdout, os.Stdout, os.Stderr)

	clientList := list.New()
	in := make(chan DataSend)

	go IOHandler(in, clientList)
	go pingpong(in, clientList)

	roomChanRead := make(chan TRoomRead)
	roomChanAdd := make(chan TRoomAdd)
	roomChanRemove := make(chan TRoomRemove)
	go goRoom(roomChanRead, roomChanAdd, roomChanRemove)
	go Cli(clientList, roomChanRead)

	service := ":9999"
	tcpAddr, error := net.ResolveTCPAddr("tcp", service)
	if error != nil {
		Error.Println("Error: Could not resolve address")
	} else {
		netListen, error := net.Listen(tcpAddr.Network(), tcpAddr.String())
		if error != nil {
			Error.Println(error)
		} else {
			defer netListen.Close()

			for {
				Info.Println("Waiting for clients")
				connection, error := netListen.Accept()
				if error != nil {
					Error.Println("Client error: ", error)
				} else {
					go ClientHandler(connection, in, clientList, roomChanRead, roomChanAdd, roomChanRemove)
				}
			}
		}
	}
}

func Cli(clientList *list.List, roomChanRead chan TRoomRead) {
	var i int
	for {
		fmt.Scanf("%d", &i)
		Info.Println("CLI clientList len: ", clientList.Len())
		for e := clientList.Front(); e != nil; e = e.Next() {
			c := e.Value.(*Client)
			Info.Println("CLI: clientList room:, ", c.Room, ", name:", c.Name)
			Info.Println("CLI: client.MuteList: ", c.MuteList)

		}
	}
}

func (c *Client) Read(buffer []byte) int {
	bytesRead, error := c.Conn.Read(buffer)
	if error != nil {
		c.Close()
		Error.Println(error)
		return -1
	}
	Trace.Println("Read ", bytesRead, " bytes")
	return bytesRead
}

func (c *Client) Close() {
	c.Quit <- true
	c.Conn.Close()
	c.RemoveMe()
}

func (c *Client) Equal(other *Client) bool {
	if bytes.Equal([]byte(c.Name), []byte(other.Name)) {
		if c.Conn == other.Conn {
			return true
		}
	}
	return false
}

func (c *Client) RemoveMe() {
	for entry := c.ClientList.Front(); entry != nil; entry = entry.Next() {
		client := entry.Value.(*Client)
		if c.Equal(client) {
			Info.Println("RemoveMe: ", c.Name)
			c.roomRemove <- TRoomRemove{c}
			c.ClientList.Remove(entry)
		}
	}
}

func IOHandler(Incoming <-chan DataSend, clientList *list.List) {
	type RLOCK struct {
		isLock bool
		Name   string
	}
	room_lock := make(map[string]RLOCK)

	for {
		Trace.Println("IOHandler: Waiting for input")
		input := <-Incoming
		Trace.Println("IOHandler: Handling \n", hex.Dump([]byte(input.Data)))
		for e := clientList.Front(); e != nil; e = e.Next() {
			client := e.Value.(*Client)
			if input.Name == client.Name {
				Trace.Println("IOHandler: skip, input.Name == client.Name, Name: ", input.Name)
				continue
			}
			if _, ok := client.MuteList[input.Name]; ok {
				Info.Println("IOHandler: skip, Mute from ", input.Name, " to ", client.Name)
				continue
			}
			if input.Room != client.Room && input.Room != ROOM_ALL {
				Trace.Println("IOHandler: skip sending to ", client.Name, ", input.Room != client.Room && input.Room != ROOM_ALL, input.Room: ", input.Room, ", client.Room: ", client.Room)
				continue
			}

			if input.Lock {
				if _, ok := room_lock[input.Room]; ok {
					if room_lock[input.Room].isLock && room_lock[input.Room].Name != input.Name {
						Info.Println("IOHandler: skip, Room lock by ", room_lock[input.Room].Name)
						continue
					}
				} else {
					Info.Println("IOHandler: Room  ", input.Room, ", locking by ", input.Name)
					room_lock[input.Room] = RLOCK{true, input.Name}
				}

			}

			if input.Unlock && room_lock[input.Room].Name == input.Name {
				Info.Println("IOHandler: Room  ", input.Room, ", unlocked by ", input.Name)
				delete(room_lock, input.Room)
			}

			client.Incoming <- input
		}
	}
}

func SendRoomList(client *Client) {
	client.Incoming <- DataSend{SERVER_NAME, client.Room,
		string([]byte{0x14, 0x04, 0x00, 0x00, 0x00,
			0x04, 0x54, 0x45, 0x53, 0x54,
			0x08, 0x54, 0x48, 0x41, 0x49, 0x4c, 0x41, 0x4e, 0x44,
			0x02, 0x5a, 0x31,
			0x02, 0x5a, 0x32}), false, false}
}

func GetRoomMembers(client *Client) {
	Trace.Println("GetRoomMembers call, client.Name: ", client.Name, " client.Room: ", client.Room)
	if len(client.Room) < 1 {
		return
	}
	chanClientList := make(chan *list.List)
	client.roomRead <- TRoomRead{client.Room, chanClientList}

	clientList := <-chanClientList

	clientCount := clientList.Len()
	Trace.Println("GetRoomMembers: len: ", clientCount)

	if clientCount > 0 {
		tmp := ""
		for e := clientList.Front(); e != nil; e = e.Next() {
			c := e.Value.(*Client)
			Trace.Println("GetRoomMembers room:, ", c.Room, ", name:", c.Name)
			tmp += string([]byte{0x00, 0x00, 0x00, 0x00, 0x00}) + string(len(c.Name)) + c.Name + string(len(c.Message)) + c.Message
		}

		client.Incoming <- DataSend{
			SERVER_NAME,
			client.Room,
			string(0x16) + string(clientCount) + string([]byte{0x00, 0x00}) + tmp + string(0x00), false, false}
	}
}

func ClearOldRoomMembers(oldRoom string, client *Client) {
	Trace.Println("ClearOldRoomMembers call, client.Name: ", client.Name, " oldRoom: ", oldRoom)
	if len(client.Room) < 1 {
		return
	}
	chanClientList := make(chan *list.List)
	client.roomRead <- TRoomRead{oldRoom, chanClientList}

	clientList := <-chanClientList

	clientCount := clientList.Len()
	Trace.Println("ClearOldRoomMembers: len: ", clientCount)

	if clientCount > 0 {
		tmp := ""
		i := 0
		for e := clientList.Front(); e != nil; e = e.Next() {
			c := e.Value.(*Client)
			if c.Name == client.Name {
				Trace.Println("ClearOldRoomMembers room:, ", c.Room, ", skip name:", c.Name)
				continue
			}

			i++
			Trace.Println("ClearOldRoomMembers room:, ", c.Room, ", name:", c.Name)
			tmp += string([]byte{0x00, 0x01, 0x00, 0x00, 0x00}) + string(len(c.Name)) + c.Name

		}

		client.Incoming <- DataSend{
			SERVER_NAME,
			client.Room,
			string(0x16) + string(i) + string([]byte{0x00, 0x00}) + tmp + string(0x00), false, false}
	}
}

func SendMyNameToJoinRoom(client *Client) {
	Trace.Println("SendMyNameToRoom: client.Room: ", client.Room)
	client.Outgoing <- DataSend{
		SERVER_NAME,
		client.Room,
		string(0x16) + string([]byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}) +
			string(len(client.Name)) + client.Name + string(len(client.Message)) + client.Message + string(0x00), false, false}
}

func SendMyNameToLeftRoom(leftRoom string, client *Client) {
	Trace.Println("SendMyNameToLeftRoom: leftRoom: ", leftRoom)
	client.Outgoing <- DataSend{
		SERVER_NAME,
		leftRoom,
		string(0x16) + string([]byte{0x01, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00}) +
			string(len(client.Name)) + client.Name + string(0x00), false, false}
}

func SendMyPttToRoom(client *Client) {
	if client.Room == "" || client.Name == "" {
		Trace.Println("SendMyPttToRoom return: ", client.Room, client.Name)
		return
	}

	Trace.Println("SendMyPttToRoom: ", client.Room)
	client.Outgoing <- DataSend{
		client.Name,
		client.Room,
		string(0x16) + string([]byte{0x01, 0x00, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00}) +
			string(len(client.Name)) + client.Name + string(0x00), true, false}
}

func SendMyReleaseToRoom(client *Client) {
	if client.Room == "" || client.Name == "" {
		Trace.Println("SendMyReleaseToRoom return: ", client.Room, client.Name)
		return
	}

	Trace.Println("SendMyReleaseToRoom: ", client.Room)
	client.Outgoing <- DataSend{
		client.Name,
		client.Room,
		string(0x16) + string([]byte{0x01, 0x00, 0x00, 0x00, 0x03, 0x00, 0x00, 0x00}) +
			string(len(client.Name)) + client.Name + string(0x00), true, true}

	client.Outgoing <- DataSend{
		SERVER_NAME,
		client.Room,
		string([]byte{0x08}), true, true}

	client.Outgoing <- DataSend{
		SERVER_NAME,
		client.Room,
		string([]byte{0x06, 0x00}), true, true}

}

func SendServerInfo(client *Client) {
	msg := "OSQe Server v0.0.1"
	tmp := string([]byte{0x00, 0x00, 0x00, 0x00, 0x00}) + string(len(SERVER_NAME)) + SERVER_NAME + string(len(msg)) + msg

	client.Incoming <- DataSend{
		SERVER_NAME,
		ROOM_ALL,
		string(0x16) + string([]byte{0x01, 0x00, 0x00}) + tmp + string(0x00), false, false}
}

func SendClientError(client *Client, msg string) {
	tmp := string([]byte{0x00, 0x00, 0x00, 0x00, 0x00}) + string(len("!Error!")) + "!Error!" + string(len(msg)) + msg

	client.Incoming <- DataSend{
		SERVER_NAME,
		ROOM_ALL,
		string(0x16) + string([]byte{0x01, 0x00, 0x00}) + tmp + string(0x00), false, false}
}

func CheckExistName(clientList *list.List, name string) bool {
	for e := clientList.Front(); e != nil; e = e.Next() {
		client := e.Value.(*Client)
		if name == client.Name {
			return true
		}
	}
	return false
}

func ClientReader(client *Client) {
	SendRoomList(client)

	buffer := make([]byte, 512)
	bufs := &bytes.Buffer{}
	read_multibyte := false
	find_cmd_token := 0

	for {
		n := client.Read(buffer)
		/*Trace.Println("size len :", len(buffer))
		Trace.Println("size n: ", n)*/
		if n < 1 {
			Error.Println("ClientReader received zero")
			return
		}

		for in := 0; in < n; in++ {
			if read_multibyte == false {
				Trace.Println("single byte mode", client.Conn.RemoteAddr().String()+"\n", hex.Dump(buffer[in:in+1]))
				// single byte mode
				switch buffer[in] {
				case 0x01: // voice
					SendMyPttToRoom(client)
					read_multibyte = true
					find_cmd_token = 0x01
				case 0x02: // ignore
					continue
				case 0x0d: // release ptt
					SendMyReleaseToRoom(client)
					// clear key ppt code
					continue
				case 0x0a:
					read_multibyte = true
					find_cmd_token = 0xa
					bufs.Reset()
					bufs.WriteByte(buffer[in])
				case 0x1a: // join/change room
					read_multibyte = true
					find_cmd_token = 0x1a
					bufs.Reset()
					bufs.WriteByte(buffer[in])
				case 0x15: // client to server
					read_multibyte = true
					find_cmd_token = 0x15
					bufs.Reset()
					bufs.WriteByte(buffer[in])
				}
			} else {
				// multi byte mode
				bufs.WriteByte(buffer[in])
				Trace.Println("multi byte mode len:", len(bufs.Bytes()), client.Conn.RemoteAddr().String()+"\n", hex.Dump(bufs.Bytes()))

				switch find_cmd_token {
				case 0x0a:
					if len(bufs.Bytes()) == 5 {
						if bytes.Equal(bufs.Bytes(), []byte{0xa, 0x82, 0x00, 0x00, 0x00}) {
							bufs.Reset()
							Trace.Println("send 0xa, 0xfa, 0x00, 0x00, 0x00.")
							client.Incoming <- DataSend{"", "", string([]byte{0xa, 0xfa, 0x00, 0x00, 0x00}), false, false}
							find_cmd_token = 0
							read_multibyte = false
							SendServerInfo(client)
						}
					}
					continue
				case 0x15: //ignore
					if len(bufs.Bytes()) == 9 && bytes.Equal(bufs.Bytes()[0:5], []byte{0x15, 0x82, 0x00, 0x00, 0x00}) {
						Trace.Println("0x15 bufs.Reset() len:", len(bufs.Bytes()), client.Conn.RemoteAddr().String()+"\n", hex.Dump(bufs.Bytes()))
						bufs.Reset()
						find_cmd_token = 0
						read_multibyte = false
						continue
					}
				case 0x1a:
					join, success := find_token_join(bufs)
					if success {
						oldRoom := client.Room
						loop_lock := false
						if len(join.Name) > MAX_NAME_LENGHT {
							SendClientError(client, "Name lenght over "+strconv.Itoa(MAX_NAME_LENGHT)+" charactor: "+string(join.Name))
							loop_lock = true
						}
						if len(join.Room) > MAX_ROOM_LENGHT {
							SendClientError(client, "Room lenght over "+strconv.Itoa(MAX_ROOM_LENGHT)+" charactor: "+string(join.Room))
							loop_lock = true
						}
						if len(join.Message) > MAX_MESSAGE_LENGHT {
							SendClientError(client, "Message lenght over "+strconv.Itoa(MAX_MESSAGE_LENGHT)+" charactor: "+string(join.Message))
							loop_lock = true
						}
						if len(join.Password) > MAX_PASSWORD_LENGHT {
							SendClientError(client, "Password lenght over "+strconv.Itoa(MAX_PASSWORD_LENGHT)+" charactor: "+string(join.Password))
							loop_lock = true
						}

						if client.Name != string(join.Name) && CheckExistName(client.ClientList, string(join.Name)) {
							SendClientError(client, "Name \""+string(join.Name)+"\" are using by other client, please change your name and try again.")
							loop_lock = true
						}

						for loop_lock {
							n := client.Read(buffer)
							/*Trace.Println("size len :", len(buffer))
							Trace.Println("size n: ", n)*/
							if n < 1 {
								Error.Println("ClientReader received zero")
								return
							}
						}

						if parseMessageCommand(client, string(join.Message)) {
							client.Message = string(join.Message)
						}

						client.Name = string(join.Name)
						client.Room = string(join.Room)
						client.Password = string(join.Password)
						if len(oldRoom) > 0 && oldRoom != client.Room {
							ClearOldRoomMembers(oldRoom, client)
						}
						client.roomRemove <- TRoomRemove{client}
						client.roomAdd <- TRoomAdd{client.Room, client}
						GetRoomMembers(client)
						SendMyNameToJoinRoom(client)
						bufs.Reset()
						find_cmd_token = 0
						read_multibyte = false
						continue
					}

				case 0x01: // voice
					if len(bufs.Bytes()) == 198 {
						client.Outgoing <- DataSend{client.Name, client.Room, string(0x01) + string(bufs.Bytes()), true, false}
						bufs.Reset()
						find_cmd_token = 0
						read_multibyte = false
					}

				}
			}

		}
		/*
			for i := 0; i < 512; i++ {
				buffer[i] = 0x00
			}
		*/
	}
}

func parseMessageCommand(client *Client, msg string) bool {
	if len(msg) < 2 || string(msg[0]) != "/" {
		return true
	}

	if len(msg) > 4 && string(msg[:5]) == "/quit" {
		client.Close()
	}

	if len(msg) > 6 && string(msg[:5]) == "/mute" {
		name := string(msg[6:])
		Info.Println("Mute: ", name)
		client.MuteList[name] = client.MuteList[name]
	}

	if len(msg) > 8 && string(msg[:7]) == "/unmute" {
		name := string(msg[8:])
		Info.Println("UnMute: ", name)
		delete(client.MuteList, name)
	}

	return false

}

func ClientSender(client *Client) {
	for {
		select {
		case buffer := <-client.Incoming:
			Trace.Println("ClientSender len:", len(buffer.Data), " To: ", client.Conn.RemoteAddr().String(), " From: ", buffer.Name, "\n", hex.Dump([]byte(buffer.Data)))
			client.Conn.Write([]byte(buffer.Data)[0:len(buffer.Data)])
		case <-client.Quit:
			Trace.Println("Client ", client.Name, " quitting")
			client.roomRemove <- TRoomRemove{client}
			client.Conn.Close()

			break
		}
	}
}

func ClientHandler(conn net.Conn, ch chan DataSend, clientList *list.List, roomRead chan TRoomRead, roomAdd chan TRoomAdd, roomRemove chan TRoomRemove) {
	Trace.Println("start go rutine for client: ", conn.RemoteAddr().String())

	newClient := &Client{"_NO_NAME_", "_NO_ROOM_", "", "", make(chan DataSend), ch, conn, make(chan bool), clientList, make(map[string]struct{}), roomRead, roomAdd, roomRemove}
	go ClientSender(newClient)
	go ClientReader(newClient)
	clientList.PushBack(newClient)
}

func goRoom(roomRead <-chan TRoomRead, roomAdd <-chan TRoomAdd, roomRemove <-chan TRoomRemove) {
	roomtList := make(map[string]Room)

	for {
		Trace.Println("goRoomUpdateClient: Waiting for input")
		select {
		case read := <-roomRead:
			Trace.Println("goRoomUpdateClient: roomRead, Roomname: ", read.RoomName)
			read.ClientList <- roomReadMember(read.RoomName, roomtList)
		case add := <-roomAdd:
			Trace.Println("goRoomUpdateClient: roomAdd, ", add.client.Room, add.client.Name)
			roomAddMember(add.client, roomtList)
		case remove := <-roomRemove:
			Trace.Println("goRoomUpdateClient: roomRemove, ", remove.client.Room, remove.client.Name)
			roomRemoveMember(remove.client, roomtList)
		}
	}
}

func roomReadMember(roomName string, roomtList map[string]Room) *list.List {
	//roomName := client.Room
	_, ok := roomtList[roomName]
	if ok {
		Trace.Println("found room name: ", roomName)
		Trace.Println("roomtList members len :", roomtList[roomName].ClientList.Len())
		return roomtList[roomName].ClientList
	} else {
		Trace.Println("room not found room name: ", roomName)
		return list.New()
	}

}

func roomRemoveMember(client *Client, roomtList map[string]Room) {
	// remove client name from all room
	Trace.Println("roomRemoveMember, client name: ", client.Name)
	for key, value := range roomtList {
		Trace.Println("roomUpdateClient, size1: ", value.ClientList.Len())
		for e := value.ClientList.Front(); e != nil; e = e.Next() {
			rclient := e.Value.(*Client)
			if rclient.Name == client.Name {
				Trace.Println("roomUpdateClient remove, client name: ", client.Name, " from room name: ", key)
				roomtList[key].ClientList.Remove(e)
				SendMyNameToLeftRoom(key, client)
			}
		}
		Trace.Println("roomUpdateClient, size2: ", value.ClientList.Len())

	}

}

func roomAddMember(client *Client, roomtList map[string]Room) {

	// add client name to room
	room, ok := roomtList[client.Room]
	if ok {
		fmt.Println("found room name: ", room.Name)
		roomtList[client.Room].ClientList.PushBack(client)
	} else {
		fmt.Println("room not found, create new room name: ", client.Room)
		roomtList[client.Room] = Room{client.Room, list.New()}
		roomtList[client.Room].ClientList.PushBack(client)
	}

}

func pingpong(Incoming <-chan DataSend, clientList *list.List) {
	for {
		Trace.Println("ping/pong")
		for e := clientList.Front(); e != nil; e = e.Next() {
			client := e.Value.(*Client)
			Trace.Println("ping: ", client.Name)
			client.Incoming <- DataSend{SERVER_NAME, ROOM_ALL, string([]byte{0x0c}), false, false}
		}
		time.Sleep(time.Second * 30)
	}
}

func inputHandle() {

}

func find_token_join(bufs *bytes.Buffer) (Join, bool) {
	Trace.Println("Find token join len:", len(bufs.Bytes()), "\n", hex.Dump(bufs.Bytes()))
	if len(bufs.Bytes()) > 1 {
		nick_lenght := int(bufs.Bytes()[1])
		Trace.Println("0x1a: nick lenght ", nick_lenght)
		if len(bufs.Bytes()) > 2+nick_lenght {
			offset_a := 2
			offset_b := 2 + nick_lenght
			nick := bufs.Bytes()[offset_a:offset_b]
			Trace.Println("0x1a: nick ", nick, string(nick))

			room_lenght := int(bufs.Bytes()[2+nick_lenght])

			Trace.Println("0x1a: room lenght ", room_lenght)
			if len(bufs.Bytes()) > 3+nick_lenght+room_lenght {
				offset_a := 3 + nick_lenght
				offset_b := 3 + nick_lenght + room_lenght
				room := bufs.Bytes()[offset_a:offset_b]
				Trace.Println("0x1a: room ", room, string(room))

				message_lenght := int(bufs.Bytes()[3+nick_lenght+room_lenght])

				Trace.Println("0x1a: message lenght ", message_lenght)
				Trace.Println("0x1a: message, len(bufs.Bytes()): ", len(bufs.Bytes()), "sum: ", 4+nick_lenght+room_lenght+message_lenght)
				if len(bufs.Bytes()) > 4+nick_lenght+room_lenght+message_lenght {

					offset_a := 4 + nick_lenght + room_lenght
					offset_b := 4 + nick_lenght + room_lenght + message_lenght
					message := bufs.Bytes()[offset_a:offset_b]
					Trace.Println("0x1a: message ", message, string(message))

					password_lenght := int(bufs.Bytes()[4+nick_lenght+room_lenght+message_lenght])

					Trace.Println("0x1a: password lenght ", password_lenght)
					if password_lenght == 0 {
						return Join{nick, room, message, nil}, true
					}
					if len(bufs.Bytes()) > 4+nick_lenght+room_lenght+message_lenght+password_lenght {
						offset_a := 5 + nick_lenght + room_lenght + message_lenght
						offset_b := 5 + nick_lenght + room_lenght + message_lenght + password_lenght
						password := bufs.Bytes()[offset_a:offset_b]
						Trace.Println("0x1a: password ", password, string(password))
						return Join{nick, room, message, password}, true
					}
				}
			}
		}
	}
	Trace.Println("Find token join false.")
	return Join{}, false
}

func checkError(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Fatal error: %s", err.Error())
		os.Exit(1)
	}
}
