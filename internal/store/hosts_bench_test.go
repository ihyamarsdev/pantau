package store

import (
	"fmt"
	"testing"
)

// BenchmarkListHosts measures the fleet-wide host query that the dashboard runs on every refresh.
func BenchmarkListHosts(b *testing.B) {
	db, err := Open(TemplateCopy(b))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { db.Close() })

	const hosts = 200
	for i := 0; i < hosts; i++ {
		if _, err := db.CreateHost(&Host{
			Name: fmt.Sprintf("host-%03d", i),
			Host: fmt.Sprintf("10.0.%d.%d", i/256, i%256),
			Port: 22,
			User: "root",
		}); err != nil {
			b.Fatal(err)
		}
	}

	b.ReportAllocs()
	for b.Loop() {
		got, err := db.ListHosts()
		if err != nil {
			b.Fatal(err)
		}
		if len(got) != hosts {
			b.Fatalf("ListHosts returned %d hosts, want %d", len(got), hosts)
		}
	}
}
