"""A minimal stand-in for the CloudAxion API, enough to drive a real
tofu plan/apply/destroy against the provider without a live account.

Mirrors the quirks recorded in docs/api-notes.md: apikey header auth,
location-scoped paths, network creation taking its name as a query parameter,
and CloudAxion allocating the VLAN and subnet itself.
"""
import json
import re
import uuid
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import urlparse, parse_qs

NETWORKS = {}
NEXT_VLAN = [104]

LOCATIONS = [
    {"display_name": "Tunis", "is_default": True, "is_preferred": True,
     "description": "Primary Tunisian location", "order_nr": 1,
     "slug": "tun1", "country_code": "tn"},
    {"display_name": "Tunis 2", "is_default": False, "is_preferred": False,
     "description": "Secondary", "order_nr": 2, "slug": "tun2", "country_code": "tn"},
]

NET_COLLECTION = re.compile(r"^/v1/(?P<slug>[^/]+)/network/network$")
NET_ITEM = re.compile(r"^/v1/(?P<slug>[^/]+)/network/network/(?P<uuid>[^/]+)$")


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass  # keep the tofu output readable

    def _send(self, status, payload=None):
        body = b"" if payload is None else json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        if body:
            self.wfile.write(body)

    def _error(self, status, message):
        self._send(status, {"errors": {"Error": message}})

    def _authorised(self):
        if self.headers.get("apikey"):
            return True
        self._error(401, "missing apikey header")
        return False

    def _body(self):
        length = int(self.headers.get("Content-Length") or 0)
        if not length:
            return {}
        try:
            return json.loads(self.rfile.read(length))
        except json.JSONDecodeError:
            return {}

    def do_GET(self):
        if not self._authorised():
            return
        path = urlparse(self.path).path

        if path == "/v1/config/locations":
            return self._send(200, LOCATIONS)

        m = NET_ITEM.match(path)
        if m:
            network = NETWORKS.get(m.group("uuid"))
            if not network:
                return self._error(404, "network not found")
            return self._send(200, network)

        self._error(404, "no route for " + path)

    def do_POST(self):
        if not self._authorised():
            return
        parsed = urlparse(self.path)

        m = NET_COLLECTION.match(parsed.path)
        if m:
            name = (parse_qs(parsed.query).get("name") or [""])[0]
            if not name:
                return self._error(400, "name is required")
            vlan = NEXT_VLAN[0]
            NEXT_VLAN[0] += 1
            network = {
                "uuid": str(uuid.uuid4()),
                "name": name,
                "type": "private",
                "vlan_id": vlan,
                "subnet": "10.1.%d.0/24" % vlan,
                "subnet_ipv6": "2a05:1cc0:10:30::40:0/112",
                "is_default": len(NETWORKS) == 0,
                "vm_uuids": [],
                "resources_count": 0,
                "created_at": "2026-08-26 10:00:00",
                "updated_at": "2026-08-26 10:00:00",
            }
            NETWORKS[network["uuid"]] = network
            return self._send(200, network)

        self._error(404, "no route for " + parsed.path)

    def do_PATCH(self):
        if not self._authorised():
            return
        path = urlparse(self.path).path

        m = NET_ITEM.match(path)
        if m:
            network = NETWORKS.get(m.group("uuid"))
            if not network:
                return self._error(404, "network not found")
            body = self._body()
            if "name" in body:
                network["name"] = body["name"]
                network["updated_at"] = "2026-08-26 11:00:00"
            return self._send(200, network)

        self._error(404, "no route for " + path)

    def do_DELETE(self):
        if not self._authorised():
            return
        path = urlparse(self.path).path

        m = NET_ITEM.match(path)
        if m:
            if NETWORKS.pop(m.group("uuid"), None) is None:
                return self._error(404, "network not found")
            return self._send(204)

        self._error(404, "no route for " + path)


if __name__ == "__main__":
    HTTPServer(("127.0.0.1", 8099), Handler).serve_forever()
