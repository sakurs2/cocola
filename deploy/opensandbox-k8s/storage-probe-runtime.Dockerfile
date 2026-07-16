FROM scratch
COPY storage-probe /storage-probe
EXPOSE 8095
ENTRYPOINT ["/storage-probe"]
