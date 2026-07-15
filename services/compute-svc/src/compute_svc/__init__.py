"""Compute domain service.

Owns VM-provisioning logic. WaaS calls the HTTP endpoint via api_call; it does
not import this module. The owning service holds the business logic.
"""
