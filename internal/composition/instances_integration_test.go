//go:build integration

package composition_test

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestInstancesThreeProcessesTwoBetsOnHundred(t *testing.T) {
	t.Parallel()
	c := startCluster(t)
	playerID := uniqueID(t, "player-")
	wallet := c.openWallet(t, c.inst(0), playerID, "100.00")

	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make([]httpResult, 2)
	exts := []string{uniqueID(t, "bet-a-"), uniqueID(t, "bet-b-")}
	for i, ext := range exts {
		wg.Add(1)
		go func(i int, ext string) {
			defer wg.Done()
			<-start
			results[i] = c.submitBet(t, c.inst(i+1), playerID, wallet.ID, ext, "80.00")
		}(i, ext)
	}
	close(start)
	wg.Wait()

	var ok, rejected int
	for i, res := range results {
		switch res.Status {
		case http.StatusOK:
			ok++
			var op operationJSON
			decodeJSON(t, res.Body, &op)
			if op.Status != "PROCESSED" || op.IdempotentReplay {
				t.Fatalf("ok result %+v", op)
			}
		case http.StatusUnprocessableEntity:
			rejected++
			var op operationJSON
			decodeJSON(t, res.Body, &op)
			if op.Status != "REJECTED" || op.FailureCode != "INSUFFICIENT_FUNDS" {
				t.Fatalf("rejected result %+v body %s", op, res.Body)
			}
		default:
			t.Fatalf("instance result %d status %d %s", i, res.Status, res.Body)
		}
	}
	if ok != 1 || rejected != 1 {
		t.Fatalf("ok=%d rejected=%d", ok, rejected)
	}

	got := c.getWallet(t, c.inst(0), wallet.ID)
	if got.Balance.Amount != "20.00" {
		t.Fatalf("balance = %s", got.Balance.Amount)
	}
	if c.debitCount(t, wallet.ID) != 1 {
		t.Fatalf("debits = %d, want 1", c.debitCount(t, wallet.ID))
	}

	for i, ext := range exts {
		replay := c.submitBet(t, c.inst(0), playerID, wallet.ID, ext, "80.00")
		if replay.Status != results[i].Status {
			t.Fatalf("replay status %d, want %d (%s)", replay.Status, results[i].Status, replay.Body)
		}
		var op operationJSON
		decodeJSON(t, replay.Body, &op)
		if !op.IdempotentReplay {
			t.Fatalf("replay not marked: %+v", op)
		}
	}
	if c.getWallet(t, c.inst(2), wallet.ID).Balance.Amount != "20.00" {
		t.Fatal("replay changed the balance")
	}
	if c.debitCount(t, wallet.ID) != 1 {
		t.Fatal("replay added a debit")
	}
	c.assertReconciled(t, c.inst(2), wallet.ID)
}

func TestInstancesConcurrentSameBet(t *testing.T) {
	t.Parallel()
	c := startCluster(t)
	playerID := uniqueID(t, "player-")
	wallet := c.openWallet(t, c.inst(0), playerID, "100.00")
	ext := uniqueID(t, "same-")

	const n = 50
	start := make(chan struct{})
	var wg sync.WaitGroup
	var firstTime atomic.Int64
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			res := c.submitBet(t, c.inst(i), playerID, wallet.ID, ext, "10.00")
			if res.Status != http.StatusOK {
				t.Errorf("status %d %s", res.Status, res.Body)
				return
			}
			var op operationJSON
			decodeJSON(t, res.Body, &op)
			if !op.IdempotentReplay {
				firstTime.Add(1)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	if firstTime.Load() != 1 {
		t.Fatalf("first-time processed = %d, want 1", firstTime.Load())
	}
	if c.getWallet(t, c.inst(1), wallet.ID).Balance.Amount != "90.00" {
		t.Fatalf("balance = %s", c.getWallet(t, c.inst(1), wallet.ID).Balance.Amount)
	}
	if c.debitCount(t, wallet.ID) != 1 {
		t.Fatalf("debits = %d, want 1", c.debitCount(t, wallet.ID))
	}
	c.assertReconciled(t, c.inst(2), wallet.ID)
}

func TestInstancesDistinctWalletsParallel(t *testing.T) {
	t.Parallel()
	c := startCluster(t)
	type opened struct {
		player string
		id     string
	}
	wallets := make([]opened, 2)
	for i := range wallets {
		player := uniqueID(t, "player-")
		w := c.openWallet(t, c.inst(i), player, "50.00")
		wallets[i] = opened{player: player, id: w.ID}
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, w := range wallets {
		wg.Add(1)
		go func(i int, w opened) {
			defer wg.Done()
			<-start
			res := c.submitBet(t, c.inst(i), w.player, w.id, uniqueID(t, "par-"), "10.00")
			if res.Status != http.StatusOK {
				t.Errorf("wallet %d status %d %s", i, res.Status, res.Body)
			}
		}(i, w)
	}
	close(start)
	wg.Wait()
	for i, w := range wallets {
		got := c.getWallet(t, c.inst(2), w.id)
		if got.Balance.Amount != "40.00" {
			t.Errorf("wallet %d balance %s", i, got.Balance.Amount)
		}
		c.assertReconciled(t, c.inst(2), w.id)
	}
}

func TestInstancesHTTPxSQSSameKeyOneDebit(t *testing.T) {
	t.Parallel()
	c := startCluster(t)
	playerID := uniqueID(t, "player-")
	wallet := c.openWallet(t, c.inst(0), playerID, "100.00")
	ext := uniqueID(t, "ext-")
	key := "provider-a:" + ext

	res := c.submitBet(t, c.inst(1), playerID, wallet.ID, ext, "20.00")
	if res.Status != http.StatusOK {
		t.Fatalf("http bet status %d %s", res.Status, res.Body)
	}

	msgID := uniqueID(t, "msg-")
	body := inboundEnvelope(msgID, "provider-a", wallet.ID, playerID, ext, key, "20.00")
	sendCtx, sendCancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer sendCancel()
	if err := c.sqs.SendWager(sendCtx, string(body), wallet.ID, msgID); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if c.inboxCount(t) == 1 && c.debitCount(t, wallet.ID) == 1 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if c.inboxCount(t) != 1 {
		t.Fatalf("inbox = %d", c.inboxCount(t))
	}
	if c.debitCount(t, wallet.ID) != 1 {
		t.Fatalf("debits = %d, want 1", c.debitCount(t, wallet.ID))
	}
	if c.ledgerCount(t, wallet.ID) != 2 {
		t.Fatalf("ledger = %d, want 2", c.ledgerCount(t, wallet.ID))
	}
	if c.getWallet(t, c.inst(2), wallet.ID).Balance.Amount != "80.00" {
		t.Fatalf("balance = %s", c.getWallet(t, c.inst(2), wallet.ID).Balance.Amount)
	}
	c.assertReconciled(t, c.inst(2), wallet.ID)
}

func TestInstancesKillPendingResumed(t *testing.T) {
	t.Parallel()
	c := startCluster(t)
	playerID := uniqueID(t, "player-")
	wallet := c.openWallet(t, c.inst(0), playerID, "100.00")
	betExt := uniqueID(t, "bet-")
	refundExt := uniqueID(t, "refund-")

	refundRes := c.do(t, c.inst(0), http.MethodPost, "/wagering/transactions", c.provider, "provider-a:"+refundExt, map[string]any{
		"providerId":                     "provider-a",
		"externalTransactionId":          refundExt,
		"playerId":                       playerID,
		"walletId":                       wallet.ID,
		"roundId":                        "round-1",
		"gameId":                         "game-1",
		"kind":                           "REFUND",
		"money":                          map[string]string{"amount": "25.00", "currency": "BRL"},
		"referenceExternalTransactionId": betExt,
	})
	if refundRes.Status != http.StatusAccepted {
		t.Fatalf("refund status %d %s", refundRes.Status, refundRes.Body)
	}
	var refundOp operationJSON
	decodeJSON(t, refundRes.Body, &refundOp)
	if refundOp.Status != "PENDING_REFERENCE" {
		t.Fatalf("refund %+v", refundOp)
	}

	c.instances[0].kill()

	betRes := c.submitBet(t, c.inst(1), playerID, wallet.ID, betExt, "25.00")
	if betRes.Status != http.StatusOK {
		t.Fatalf("bet status %d %s", betRes.Status, betRes.Body)
	}

	deadline := time.Now().Add(15 * time.Second)
	var last transactionJSON
	for time.Now().Before(deadline) {
		got := c.do(t, c.inst(1), http.MethodGet, "/wagering/transactions/"+refundOp.TransactionID, c.provider, "", nil)
		if got.Status == http.StatusOK {
			decodeJSON(t, got.Body, &last)
			if last.Status == "PROCESSED" {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if last.Status != "PROCESSED" {
		t.Fatalf("refund not resumed: %+v", last)
	}
	if c.getWallet(t, c.inst(2), wallet.ID).Balance.Amount != "100.00" {
		t.Fatalf("balance = %s, want 100.00 after bet+refund", c.getWallet(t, c.inst(2), wallet.ID).Balance.Amount)
	}
	if c.debitCount(t, wallet.ID) != 1 {
		t.Fatalf("debits = %d", c.debitCount(t, wallet.ID))
	}
	if c.ledgerCount(t, wallet.ID) != 3 {
		t.Fatalf("ledger = %d, want 3 (opening+debit+credit)", c.ledgerCount(t, wallet.ID))
	}

	replay := c.do(t, c.inst(2), http.MethodPost, "/wagering/transactions", c.provider, "provider-a:"+refundExt, map[string]any{
		"providerId":                     "provider-a",
		"externalTransactionId":          refundExt,
		"playerId":                       playerID,
		"walletId":                       wallet.ID,
		"roundId":                        "round-1",
		"gameId":                         "game-1",
		"kind":                           "REFUND",
		"money":                          map[string]string{"amount": "25.00", "currency": "BRL"},
		"referenceExternalTransactionId": betExt,
	})
	if replay.Status != http.StatusOK {
		t.Fatalf("refund replay status %d %s", replay.Status, replay.Body)
	}
	var replayOp operationJSON
	decodeJSON(t, replay.Body, &replayOp)
	if !replayOp.IdempotentReplay {
		t.Fatalf("expected refund replay %+v", replayOp)
	}
	c.assertReconciled(t, c.inst(2), wallet.ID)
}
