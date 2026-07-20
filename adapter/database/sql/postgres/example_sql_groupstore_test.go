package postgres_test

import (
	"context"
	stdsql "database/sql"
	"fmt"

	msgin "github.com/kartaladev/msgin"
	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
	"github.com/kartaladev/msgin/adapter/database/sql/postgres"
	"github.com/kartaladev/msgin/adapter/memory"
)

// groupStoreLineItem is one line of an order, correlated by orderID toward a
// released order total — mirrors msgin's own ExampleAggregator payload.
type groupStoreLineItem struct {
	sku   string
	price int
}

// wireGroupAggregator builds an Aggregator over store and out the SAME way
// regardless of which msgin.MessageGroupStore implementation store is: this
// wiring is the whole point of the MessageGroupStore SPI (ADR 0020/0021) — an
// Aggregator built over memory.NewGroupStore() (in-process only) and one
// built over msginsql.NewGroupStore(...) (durable, multi-process) are
// interchangeable from here down.
func wireGroupAggregator(store msgin.MessageGroupStore, out msgin.MessageChannel) (*msgin.Aggregator, error) {
	return msgin.NewAggregator[groupStoreLineItem, int](store,
		func(_ context.Context, group []msgin.Message[groupStoreLineItem]) (msgin.Message[int], error) {
			total := 0
			for _, m := range group {
				total += m.Payload().price
			}
			return msgin.New(total), nil
		},
		msgin.WithOutputChannel(out),
		msgin.WithCompletionSize(3),
	)
}

// runGroupAggregatorDemo drives 3 correlated line-items through agg and
// prints the released order total. It also starts agg.Run in the
// background: a durable store's crash recovery rides ENTIRELY on the
// reaper Run drives (see msginsql.GroupStore's "go agg.Run(ctx) is
// REQUIRED" doc) — a caller who never calls Run silently never
// crash-recovers a group stuck between ClaimGroup and SettleGroup. Showing
// it here, even though the in-memory store below never crashes
// mid-process, keeps this wiring identical to the durable one in
// Example_sqlGroupStore.
func runGroupAggregatorDemo(store msgin.MessageGroupStore) {
	out := msgin.NewDirectChannel()
	if err := out.Subscribe(msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
		fmt.Printf("order total: %v\n", m.Payload())
		return nil
	})); err != nil {
		panic(err)
	}

	agg, err := wireGroupAggregator(store, out)
	if err != nil {
		panic(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go agg.Run(ctx)

	items := []groupStoreLineItem{
		{sku: "widget", price: 10},
		{sku: "gadget", price: 25},
		{sku: "gizmo", price: 15},
	}
	for i, it := range items {
		msg := msgin.New[any](it, msgin.WithID(fmt.Sprintf("line-%d", i)), msgin.WithHeaders(map[string]any{
			msgin.HeaderCorrelationID: "order-1",
		}))
		if err := agg.Handle(context.Background(), msg); err != nil {
			panic(err)
		}
	}
}

// Example_memoryGroupStore wires an Aggregator over the in-memory
// msgin.MessageGroupStore (adapter/memory) and runs it end to end — the
// only Output-checked example in this file, so it needs no live database
// or container. See Example_sqlGroupStore below: swapping the store for a
// durable msginsql.GroupStore is the ONLY change required to make this same
// wiring durable and safe across multiple processes.
func Example_memoryGroupStore() {
	store, err := memory.NewGroupStore()
	if err != nil {
		panic(err)
	}
	runGroupAggregatorDemo(store)

	// Output:
	// order total: 50
}

// Example_sqlGroupStore is a compile-only reference for the durable,
// multi-process equivalent of Example_memoryGroupStore — it has no
// "Output:" comment, so `go test` compiles it (proving the exact call shape
// below is correct and stays correct) but never executes it, which is why db
// is left nil rather than opened against a real PostgreSQL instance: a
// caller adopting this wires db to an already-open, schema-provisioned
// *sql.DB (postgres.GroupDDL, or GroupStore.EnsureSchema for dev/test) of
// their own.
//
// The ONLY change from Example_memoryGroupStore is the store constructor:
// msginsql.NewGroupStore(db, table, postgres.GroupDialect()) in place of
// memory.NewGroupStore(). Everything below wireGroupAggregator — the
// Aggregator wiring, the release strategy, agg.Handle — is identical either
// way. What is NOT optional for the durable store is go agg.Run(ctx)
// (already present in runGroupAggregatorDemo, shared by both examples):
// msginsql.GroupStore.RecoverInterval() returns its lease TTL (not 0), so a
// group crashed between ClaimGroup and SettleGroup is only ever recovered by
// the reaper Run drives — omit Run and that crash recovery silently never
// happens.
func Example_sqlGroupStore() {
	var db *stdsql.DB // caller-provided, already open and schema-provisioned

	store, err := msginsql.NewGroupStore(db, "msgin_group", postgres.GroupDialect())
	if err != nil {
		panic(err)
	}
	runGroupAggregatorDemo(store)
}
