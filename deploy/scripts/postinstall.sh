#!/bin/sh
set -e

systemctl daemon-reload

echo ""
echo "tangra-asterisk installed successfully!"
echo ""
echo "Next steps:"
echo "  1. Edit /etc/tangra-asterisk/server.yaml and data.yaml"
echo "  2. Set environment variables in /etc/tangra-asterisk/env:"
echo "     ADMIN_GRPC_ENDPOINT=admin-service:7787"
echo "     MODULE_REGISTRATION_SECRET=your-secret"
echo "     GRPC_ADVERTISE_ADDR=this-host:9800"
echo "     ASTERISK_CDR_DSN=user:pass@tcp(freepbx:3306)/asteriskcdrdb?parseTime=true&loc=UTC"
echo "     ASTERISK_CONFIG_DSN=user:pass@tcp(freepbx:3306)/asterisk?parseTime=true&loc=UTC"
echo "  3. Place mTLS certs in /etc/tangra-asterisk/certs/ (ca.crt, server.crt, server.key)"
echo "  4. systemctl enable --now tangra-asterisk"
echo ""
