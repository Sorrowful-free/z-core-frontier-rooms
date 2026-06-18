package reservation_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	adapterreservation "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/reservation"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	benchutil "github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/bench"
)

const benchRoomCapacity = 16

func BenchmarkReservation_Reserve(b *testing.B) {
	ctx := context.Background()
	res := newTestReservation()
	const roomID = domain.RoomID(1)
	if err := res.RegisterRoom(ctx, roomID, benchRoomCapacity, ""); err != nil {
		b.Fatalf("RegisterRoom: %v", err)
	}

	expires := time.Now().Add(time.Hour)
	var peerSeq atomic.Uint64

	b.ResetTimer()
	for b.Loop() {
		peerID := domain.PeerID(peerSeq.Add(1))
		if err := res.Reserve(ctx, roomID, peerID, expires); err != nil {
			b.Fatalf("Reserve: %v", err)
		}
	}
}

func BenchmarkReservation_IssueJoinPath(b *testing.B) {
	for _, rooms := range []int{1, 32, 128} {
		b.Run(benchutil.RoomCountLabel(rooms), func(b *testing.B) {
			benchmarkIssueJoinPath(b, rooms, false)
		})
	}
}

func BenchmarkReservation_IssueJoinPath_LegacyGlobal(b *testing.B) {
	for _, rooms := range []int{1, 32, 128} {
		b.Run(benchutil.RoomCountLabel(rooms), func(b *testing.B) {
			benchmarkIssueJoinPath(b, rooms, true)
		})
	}
}

func benchmarkIssueJoinPath(b *testing.B, roomCount int, legacy bool) {
	ctx := context.Background()
	expires := time.Now().Add(time.Hour)

	var (
		modern *adapterreservation.Reservation
		old    *legacyGlobalReservation
	)
	if legacy {
		old = newLegacyGlobalReservation()
		for id := domain.RoomID(1); id <= domain.RoomID(roomCount); id++ {
			old.registerRoom(id, benchRoomCapacity)
		}
	} else {
		modern = newTestReservation()
		for id := domain.RoomID(1); id <= domain.RoomID(roomCount); id++ {
			if err := modern.RegisterRoom(ctx, id, benchRoomCapacity, ""); err != nil {
				b.Fatalf("RegisterRoom %d: %v", id, err)
			}
		}
	}

	var seq atomic.Uint64
	b.ResetTimer()
	for b.Loop() {
		n := seq.Add(1)
		roomID := domain.RoomID(n%uint64(roomCount) + 1)
		peerID := domain.PeerID(n)

		if legacy {
			if err := old.reserve(roomID, peerID, expires); err != nil {
				b.Fatalf("reserve: %v", err)
			}
			if err := old.admit(roomID, peerID); err != nil {
				b.Fatalf("admit: %v", err)
			}
			if err := old.revoke(roomID, peerID); err != nil {
				b.Fatalf("revoke: %v", err)
			}
			continue
		}

		if err := modern.Reserve(ctx, roomID, peerID, expires); err != nil {
			b.Fatalf("Reserve: %v", err)
		}
		if err := modern.Admit(ctx, roomID, peerID); err != nil {
			b.Fatalf("Admit: %v", err)
		}
		if err := modern.Revoke(ctx, roomID, peerID); err != nil {
			b.Fatalf("Revoke: %v", err)
		}
	}
}

func BenchmarkReservation_Reserve_ParallelSpread(b *testing.B) {
	for _, rooms := range []int{32, 128} {
		b.Run(benchutil.RoomCountLabel(rooms), func(b *testing.B) {
			b.Run("per_room_lock", func(b *testing.B) {
				benchmarkReserveParallelSpread(b, rooms, false)
			})
			b.Run("global_legacy", func(b *testing.B) {
				benchmarkReserveParallelSpread(b, rooms, true)
			})
		})
	}
}

func benchmarkReserveParallelSpread(b *testing.B, roomCount int, legacy bool) {
	ctx := context.Background()
	expires := time.Now().Add(time.Hour)

	var (
		modern *adapterreservation.Reservation
		old    *legacyGlobalReservation
	)
	if legacy {
		old = newLegacyGlobalReservation()
		for id := domain.RoomID(1); id <= domain.RoomID(roomCount); id++ {
			old.registerRoom(id, benchRoomCapacity)
		}
	} else {
		modern = newTestReservation()
		for id := domain.RoomID(1); id <= domain.RoomID(roomCount); id++ {
			if err := modern.RegisterRoom(ctx, id, benchRoomCapacity, ""); err != nil {
				b.Fatalf("RegisterRoom %d: %v", id, err)
			}
		}
	}

	var seq atomic.Uint64
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		worker := seq.Add(1)
		for pb.Next() {
			n := seq.Add(1)
			roomID := domain.RoomID((worker+n)%uint64(roomCount) + 1)
			peerID := domain.PeerID(n)

			if legacy {
				if err := old.reserve(roomID, peerID, expires); err != nil {
					panic(fmt.Sprintf("reserve: %v", err))
				}
				if err := old.revoke(roomID, peerID); err != nil {
					panic(fmt.Sprintf("revoke: %v", err))
				}
				continue
			}

			if err := modern.Reserve(ctx, roomID, peerID, expires); err != nil {
				panic(fmt.Sprintf("Reserve: %v", err))
			}
			if err := modern.Revoke(ctx, roomID, peerID); err != nil {
				panic(fmt.Sprintf("Revoke: %v", err))
			}
		}
	})
}

func BenchmarkReservation_Reserve_ParallelSameRoom(b *testing.B) {
	const roomID = domain.RoomID(1)
	ctx := context.Background()
	expires := time.Now().Add(time.Hour)

	b.Run("per_room_lock", func(b *testing.B) {
		res := newTestReservation()
		if err := res.RegisterRoom(ctx, roomID, benchRoomCapacity*128, ""); err != nil {
			b.Fatalf("RegisterRoom: %v", err)
		}
		runParallelReserveRevoke(b, func(peerID domain.PeerID) error {
			if err := res.Reserve(ctx, roomID, peerID, expires); err != nil {
				return err
			}
			return res.Revoke(ctx, roomID, peerID)
		})
	})

	b.Run("global_legacy", func(b *testing.B) {
		res := newLegacyGlobalReservation()
		res.registerRoom(roomID, benchRoomCapacity*128)
		runParallelReserveRevoke(b, func(peerID domain.PeerID) error {
			if err := res.reserve(roomID, peerID, expires); err != nil {
				return err
			}
			return res.revoke(roomID, peerID)
		})
	})
}

func runParallelReserveRevoke(b *testing.B, op func(domain.PeerID) error) {
	var seq atomic.Uint64
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			peerID := domain.PeerID(seq.Add(1))
			if err := op(peerID); err != nil {
				panic(err)
			}
		}
	})
}
