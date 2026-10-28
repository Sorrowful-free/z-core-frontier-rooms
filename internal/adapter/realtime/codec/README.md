# adapter/realtime/codec

Binary payload codec for state/input/RPC.

**Wire contract:** [docs/wire-state-codec.md](../../../../docs/wire-state-codec.md) — BE, maps, flags, limits.  
Client (Godot): [docs/client-protocol/README.md](../../../../docs/client-protocol/README.md), ENet: [docs/client-protocol/enet.md](../../../../docs/client-protocol/enet.md).

Implementations: `room_state_codec.go`, `peer_state_codec.go`, `entity_codec.go`, `input_state_codec.go`, `rpc_state_codec.go`, …

- `RoomState`/`RoomStatePatch` do **not** carry `password` (D2) — the room password lives in `reservation` and is not serialized on the wire.
- All `Decode*` reject payloads with trailing bytes (`checkNoTrailingBytes` in `codec_util.go`).

When the layout changes — update **both** the codec **and** `docs/wire-state-codec.md`.
