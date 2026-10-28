package docsite

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"src.elv.sh/pkg/prog"
	"src.elv.sh/pkg/testutil"
)

func TestHandler(t *testing.T) {
	srv := httptest.NewServer(Handler())
	defer srv.Close()
	body, status := get(t, srv.URL+"/")
	if _, built := Site(); built {
		if status != http.StatusOK || !strings.Contains(body, "<html") {
			t.Errorf("GET / => %d %q, want 200 and the homepage", status, body)
		}
		// The category indexes are directories with an index.html.
		if _, status := get(t, srv.URL+"/learn/"); status != http.StatusOK {
			t.Errorf("GET /learn/ => %d, want 200", status)
		}
	} else {
		if status != http.StatusNotFound || !strings.Contains(body, "not built in") {
			t.Errorf("GET / without site => %d %q, want 404 and the placeholder", status, body)
		}
		if _, status := get(t, srv.URL+"/learn/"); status != http.StatusNotFound {
			t.Errorf("GET /learn/ without site => %d, want 404", status)
		}
	}
}

func TestProgram_ServesUntilStopped(t *testing.T) {
	urlCh := make(chan string, 1)
	stop := make(chan struct{})
	p := &Program{listening: func(url string) { urlCh <- url }, stop: stop}
	exitCh := make(chan int, 1)
	devNull := devNull(t)
	go func() {
		exitCh <- prog.Run([3]*os.File{devNull, devNull, devNull},
			[]string{"elvish", "-docs", "-browser=false"}, p)
	}()
	var url string
	select {
	case url = <-urlCh:
	case <-time.After(testutil.Scaled(2 * time.Second)):
		t.Fatal("timed out waiting for the server to listen")
	}
	if !strings.HasPrefix(url, "http://127.0.0.1:") {
		t.Errorf("url is %q, want loopback", url)
	}
	if _, status := get(t, url); status != http.StatusOK && status != http.StatusNotFound {
		t.Errorf("GET %s => %d", url, status)
	}
	close(stop)
	select {
	case exit := <-exitCh:
		if exit != 0 {
			t.Errorf("exit %d, want 0", exit)
		}
	case <-time.After(testutil.Scaled(2 * time.Second)):
		t.Fatal("timed out waiting for the server to stop")
	}
}

func TestProgram_BadUsage(t *testing.T) {
	devNull := devNull(t)
	for _, args := range [][]string{
		{"elvish", "-docs", "arg"},
		{"elvish", "-docs", "-port", "x"},
	} {
		exit := prog.Run([3]*os.File{devNull, devNull, devNull}, args, &Program{})
		if exit != 2 {
			t.Errorf("%v => exit %d, want 2", args, exit)
		}
	}
}

func TestProgram_NextProgramWithoutFlag(t *testing.T) {
	p := &Program{}
	if err := p.Run([3]*os.File{}, nil); err == nil {
		t.Error("Run without -docs => nil, want NextProgram")
	}
}

func get(t *testing.T, url string) (string, int) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body), resp.StatusCode
}

func devNull(t *testing.T) *os.File {
	f, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}
