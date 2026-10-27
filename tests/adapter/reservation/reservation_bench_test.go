package reservation_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

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
		if err := res.Reserve(ctx, roomID, peerID, testNick(peerID), expires); err != nil {
			b.Fatalf("Reserve: %v", err)
		}
		if err := res.Revoke(ctx, roomID, peerID); err != nil {
			b.Fatalf("Revoke: %v", err)
		}
	}
}

func BenchmarkReservation_IssueJoinPath(b *testing.B) {
	for _, rooms := range []int{1, 32, 128} {
		b.Run(benchutil.RoomCountLabel(rooms), func(b *testing.B) {
			benchmarkIssueJoinPath(b, rooms)
		})
	}
}

func benchmarkIssueJoinPath(b *testing.B, roomCount int) {
	ctx := context.Background()
	expires := time.Now().Add(time.Hour)

	res := newTestReservation()
	for id := domain.RoomID(1); id <= domain.RoomID(roomCount); id++ {
		if err := res.RegisterRoom(ctx, id, benchRoomCapacity, ""); err != nil {
			b.Fatalf("RegisterRoom %d: %v", id, err)
		}
	}

	var seq atomic.Uint64
	b.ResetTimer()
	for b.Loop() {
		n := seq.Add(1)
		roomID := domain.RoomID(n%uint64(roomCount) + 1)
		peerID := domain.PeerID(n)

		if err := res.Reserve(ctx, roomID, peerID, testNick(peerID), expires); err != nil {
			b.Fatalf("Reserve: %v", err)
		}
		if err := res.Admit(ctx, roomID, peerID); err != nil {
			b.Fatalf("Admit: %v", err)
		}
		if err := res.Revoke(ctx, roomID, peerID); err != nil {
			b.Fatalf("Revoke: %v", err)
		}
	}
}

func BenchmarkReservation_Reserve_ParallelSpread(b *testing.B) {
	for _, rooms := range []int{32, 128} {
		b.Run(benchutil.RoomCountLabel(rooms), func(b *testing.B) {
			benchmarkReserveParallelSpread(b, rooms)
		})
	}
}

func benchmarkReserveParallelSpread(b *testing.B, roomCount int) {
	ctx := context.Background()
	expires := time.Now().Add(time.Hour)

	res := newTestReservation()
	for id := domain.RoomID(1); id <= domain.RoomID(roomCount); id++ {
		if err := res.RegisterRoom(ctx, id, benchRoomCapacity, ""); err != nil {
			b.Fatalf("RegisterRoom %d: %v", id, err)
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

			if err := res.Reserve(ctx, roomID, peerID, testNick(peerID), expires); err != nil {
				panic(fmt.Sprintf("Reserve: %v", err))
			}
			if err := res.Revoke(ctx, roomID, peerID); err != nil {
				panic(fmt.Sprintf("Revoke: %v", err))
			}
		}
	})
}

func BenchmarkReservation_Reserve_ParallelSameRoom(b *testing.B) {
	const roomID = domain.RoomID(1)
	ctx := context.Background()
	expires := time.Now().Add(time.Hour)

	res := newTestReservation()
	if err := res.RegisterRoom(ctx, roomID, domain.MaxRoomCapacity, ""); err != nil {
		b.Fatalf("RegisterRoom: %v", err)
	}
	runParallelReserveRevoke(b, func(peerID domain.PeerID) error {
		if err := res.Reserve(ctx, roomID, peerID, testNick(peerID), expires); err != nil {
			return err
		}
		return res.Revoke(ctx, roomID, peerID)
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
