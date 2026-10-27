#include <arpa/inet.h>
#include <errno.h>
#include <netinet/in.h>
#include <stdio.h>
#include <stdlib.h>
#include <sys/socket.h>
#include <unistd.h>

#ifndef SO_TIMESTAMPING
#define SO_TIMESTAMPING 37
#endif

#ifndef SOF_TIMESTAMPING_RX_SOFTWARE
#define SOF_TIMESTAMPING_RX_SOFTWARE (1 << 2)
#endif

int main(int argc, char **argv)
{
	char *end;
	long port;

	if (argc != 3) {
		fprintf(stderr, "usage: %s ADDRESS PORT\n", argv[0]);
		return 1;
	}
	errno = 0;
	port = strtol(argv[2], &end, 10);
	if (errno || *end || port < 1 || port > 65535) {
		fprintf(stderr, "invalid port: %s\n", argv[2]);
		return 1;
	}

	int tsfd = socket(AF_INET, SOCK_DGRAM, 0);
	if (tsfd >= 0) {
		int val = SOF_TIMESTAMPING_RX_SOFTWARE;
		setsockopt(tsfd, SOL_SOCKET, SO_TIMESTAMPING, &val,
			   sizeof(val));
	}

	int lfd = socket(AF_INET, SOCK_STREAM, 0);
	if (lfd < 0) {
		perror("socket");
		return 1;
	}

	int opt = 1;
	setsockopt(lfd, SOL_SOCKET, SO_REUSEADDR, &opt, sizeof(opt));

	struct sockaddr_in addr = {
		.sin_family = AF_INET,
		.sin_port = htons((uint16_t)port),
	};
	if (inet_pton(AF_INET, argv[1], &addr.sin_addr) != 1) {
		fprintf(stderr, "invalid address: %s\n", argv[1]);
		return 1;
	}
	if (bind(lfd, (struct sockaddr *)&addr, sizeof(addr)) < 0) {
		perror("bind");
		return 1;
	}
	if (listen(lfd, 5) < 0) {
		perror("listen");
		return 1;
	}

	for (;;) {
		int cfd = accept(lfd, NULL, NULL);
		if (cfd < 0)
			continue;

		usleep(50000);
		char buf[4096];
		if (recv(cfd, buf, sizeof(buf), 0) > 0) {
			usleep(100000);
			const char resp[] = "HTTP/1.0 200 "
					    "OK\r\nContent-Length: 2\r\n\r\nOK";
			send(cfd, resp, sizeof(resp) - 1, 0);
		}
		close(cfd);
	}
}
