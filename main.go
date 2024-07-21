
package main

import (
  "context"
  //"fmt"
  "log"
  //. "radeaa"
  "net"
  "net/http"
  "golang.org/x/sys/unix"
  "syscall"
)

func main() {
  // Sort this alphabetically please.
  http.HandleFunc("/api/radeaa/", Webfrickel_radeaa)
  lc := net.ListenConfig {
    Control: func(network, address string, conn syscall.RawConn) error {
      var operr error
      err := conn.Control(func(fd uintptr) {
        operr = syscall.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1)
      });
      if (err != nil) {
        log.Printf("Could not set socket option SO_REUSEADDR for webserver socket!");
        return err
      }
      if (operr != nil) {
        log.Printf("Could not set socket option SO_REUSEADDR for webserver socket!");
        return err
      }
      return nil
    },
  }
  ln, err := lc.Listen(context.Background(), "tcp", "[::1]:7338")
  if err != nil {
    log.Fatal("Listening failed!");
    panic(err)
  }
  log.Fatal(http.Serve(ln, nil)) // This normally should not exit.
}
