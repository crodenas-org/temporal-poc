"""DNS domain service.

Owns IP-reservation logic. WaaS never imports this — it calls the HTTP endpoint
via its api_call primitive. The business logic lives here, in the owning service.
"""
