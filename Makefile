.SECONDARY:

.PHONY: run-worker
run-worker: docker-autodemo-worker
	docker run \
		--rm \
		--interactive \
		--tty \
		--volume ${PWD}/mytest:/mytest  \
		--volume ${PWD}/assets:/assets  \
		--volume ${PWD}/projects:/projects  \
		--publish 8080:8080 \
		--env OPENAI_API_KEY \
		--env OPENAI_API_ORG_ID \
		--env OPENAI_API_PROJ_ID \
		--env ELEVEN_VOICE_ID \
		--env ELEVEN_API_KEY \
		autodemo-worker 

.PHONY: debug
debug: docker-autodemo-worker

.PHONY: docker-autodemo-worker
docker-autodemo-worker:
	docker image inspect \
		--format 'docker image autodemo-worker was created on {{.Created}}' autodemo-worker \
			|| docker build --tag autodemo-worker --file docker/worker .
