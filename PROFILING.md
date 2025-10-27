# Profiling the TUI

1. Build the binary:
   ```sh
   go build -o opperator ./cmd/app
   ```
2. Run the TUI with CPU profiling:
   ```sh
   ./opperator --tui-cpuprofile /tmp/opperator.cpu
   ```
   Exercise the UI, then exit to flush the profile file.
3. Inspect the capture:
   ```sh
   go tool pprof -http=:4321 ./opperator /tmp/opperator.cpu
   ```
   Use the web UI (or `top`/`list` commands) to find hotspots.
