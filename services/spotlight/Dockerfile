FROM ubuntu:18.04
RUN apt-get update
RUN apt-get install -y librsvg2-bin ca-certificates
WORKDIR /usr/spotlight
ADD bin/spotlightd  /usr/spotlight/spotlightd
ADD fonts /root/.fonts/google/
ENTRYPOINT [ "/usr/spotlight/spotlightd" ]