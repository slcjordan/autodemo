# Autodemo

Autodemo is a tool for generating video demos of curl commands automatically. This is a throwaway iteration and contains bugs.

## Getting Started

### Prerequisites

- [Go](https://go.dev/) installed
- [Make](https://www.gnu.org/software/make/)

### Running Autodemo

To start Autodemo, you need to run two services:

1. Start the worker:

   ```sh
   make run-worker
   ```

   This will start a worker on port `8080`.

2. Start the Autodemo web server:

   ```sh
   cd cmd/autodemo
   go run main.go
   ```

   This will start the web server on port `11080`.

3. Access the dashboard: Open your browser and go to [http://localhost:11080/pages/dashboard/](http://localhost:11080/pages/dashboard/).

### Environment Variables

To enable video demo creation, set the following environment variables:

```sh
export OPENAI_API_KEY=<your_openai_api_key>
export OPENAI_API_ORG_ID=<your_openai_org_id>
export OPENAI_API_PROJ_ID=<your_openai_proj_id>
export ELEVEN_VOICE_ID=<your_eleven_voice_id>
export ELEVEN_API_KEY=<your_eleven_api_key>
```

## Known Issues

- All recorded curl requests are sent to ChatGPT at once, which can easily hit API rate limits if too many requests are recorded simultaneously.

## Contributing

This project is in an early stage, and contributions are welcome. Feel free to open issues or submit pull requests.

## License

[MIT License](LICENSE)
