FROM golang:1.7.5
ADD nbrp /
ADD wait-for-it.sh /
EXPOSE 8080
