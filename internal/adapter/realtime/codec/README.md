# adapter/realtime/codec

Бинарный codec payload для state/input/RPC.

**Контракт wire:** [docs/wire-state-codec.md](../../../../docs/wire-state-codec.md) — BE, maps, flags, лимиты.  
Клиент (Godot): [docs/client-protocol/README.md](../../../../docs/client-protocol/README.md), ENet: [docs/client-protocol/enet.md](../../../../docs/client-protocol/enet.md).

Реализации: `room_state_codec.go`, `peer_state_codec.go`, `entity_codec.go`, `input_state_codec.go`, `rpc_state_codec.go`, …

При изменении layout — обновить **и** codec, **и** `docs/wire-state-codec.md`.
