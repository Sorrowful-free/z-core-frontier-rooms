package domain

// Группы OpCode (1 байт):
//   0x01–0x3F — игровой трафик / relay (зарезервировано)
//   0x40–0x4F — ошибки join / admit (клиент мапит код → UI)
//   0x50–0x5F — внутренние ошибки сервера при join
//   0x60–0x6F — ошибки внутри комнаты (отдельная задача)
// Payload для кодов ошибок join: пустой.

const (
	OpEmptyToken    OpCode = 0x40
	OpInvalidToken  OpCode = 0x41
	OpExpiredToken  OpCode = 0x42
	OpRoomNotFound  OpCode = 0x43
	OpJoinDenied    OpCode = 0x44
	OpReplaceFailed OpCode = 0x45
	OpPeerNotFound  OpCode = 0x46

	OpInternal        OpCode = 0x50
	OpPeerStartFailed OpCode = 0x51
)
