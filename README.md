hex cmd
----
0x01 voice stream & ptt
0x06 ???? / 0x0600
0x08 ????
0x0d release ptt
0x0a cmd stream
0x1a join/change channel
0x14 server send channel list to client
0x15 client send 0x82000000 and 0x???????? to server
0x16 update members list/status in channel



sever send channel lists 0x14
number of channel 1 byte  | 00 00 00 | channel lenght 1 bytes | channel name max 10 bytes| channel lenght | channel name | channel lenght | channel name | .......

server send members of channel 0x16 list terminate with 00
number of member | 00 00 00
	00 00 00 00 | name lenght 1 bytes | name max 10 bytes | status lenght | status max 100 bytes
	00 00 00 00 | name lenght 1 bytes | name max 10 bytes | status lenght | status max 100 bytes
	00 00 00 00 | name lenght 1 bytes | name max 10 bytes | status lenght | status max 100 bytes
	0x00


server send members of channel 0x16 remove member
number of member | 00 00 00
	01 00 00 00 | name lenght 1 bytes | name max 10 bytes
	01 00 00 00 | name lenght 1 bytes | name max 10 bytes
	01 00 00 00 | name lenght 1 bytes | name max 10 bytes



server send update member of channel 0x16 remove member
01               | 00 00 | 00 02 00 00 00 | 04 | 4a414e45   // ptt press
01               | 00 00 | 00 03 00 00 00 | 04 | 4a414e45   // ptt release
