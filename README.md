# store-forward-otel
A Go service that buffers OTLP spans to disk when the backend is unreachable and replays them on reconnect (the SAF pattern abstracted as a standalone library).
