.PHONY: up stop status down deploy test smoke open logs
up:
	./scripts/lab.sh up
deploy:
	./scripts/lab.sh deploy
stop:
	./scripts/lab.sh stop
status:
	./scripts/lab.sh status
down:
	./scripts/lab.sh down
open:
	./scripts/lab.sh open
test:
	GOTOOLCHAIN=go1.26.0 go test -race ./internal/lab/...
smoke:
	python3 scripts/smoke.py
logs:
	./scripts/lab.sh logs
