docker-build:
	docker build -t guesswhat .

docker-run:
	docker run -p 8080:8080 guesswhat
