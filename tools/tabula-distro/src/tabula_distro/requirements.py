"""Runtime requirement checks for distro-managed applications."""
from __future__ import annotations

import os
import shutil
from dataclasses import dataclass

from .config import DistroConfig, ExecutableRequirement


@dataclass(frozen=True)
class ExecutableStatus:
    name: str
    required: bool
    found: bool
    path: str | None = None
    required_for: tuple[str, ...] = ()
    install_hint: str = ""

    def to_json(self) -> dict:
        payload = {
            "name": self.name,
            "required": self.required,
            "found": self.found,
            "required_for": list(self.required_for),
        }
        if self.path:
            payload["path"] = self.path
        if self.install_hint:
            payload["install_hint"] = self.install_hint
        return payload


class RequirementsError(RuntimeError):
    pass


def check_executables(distro: DistroConfig, *, env: dict[str, str] | None = None) -> tuple[ExecutableStatus, ...]:
    path = (env or os.environ).get("PATH")
    statuses: list[ExecutableStatus] = []
    for requirement in distro.executable_requirements:
        found = shutil.which(requirement.name, path=path)
        statuses.append(_status(requirement, found))
    return tuple(statuses)


def require_executables(distro: DistroConfig, *, env: dict[str, str] | None = None) -> tuple[ExecutableStatus, ...]:
    statuses = check_executables(distro, env=env)
    missing_required = [status for status in statuses if status.required and not status.found]
    if missing_required:
        raise RequirementsError(_missing_message(distro.name, missing_required))
    return statuses


def warnings(statuses: tuple[ExecutableStatus, ...]) -> tuple[str, ...]:
    return tuple(
        _warning(status)
        for status in statuses
        if not status.required and not status.found
    )


def _status(requirement: ExecutableRequirement, found: str | None) -> ExecutableStatus:
    return ExecutableStatus(
        name=requirement.name,
        required=requirement.required,
        found=bool(found),
        path=found,
        required_for=requirement.required_for,
        install_hint=requirement.install_hint,
    )


def _missing_message(distro_name: str, missing: list[ExecutableStatus]) -> str:
    lines = [f"distro {distro_name!r} is missing required runtime executables:"]
    for status in missing:
        lines.append(f"- {status.name}" + (f" required for: {', '.join(status.required_for)}" if status.required_for else ""))
        if status.install_hint:
            lines.append(f"  install: {status.install_hint}")
    return "\n".join(lines)


def _warning(status: ExecutableStatus) -> str:
    message = f"optional runtime executable {status.name!r} is missing"
    if status.required_for:
        message += f"; required for: {', '.join(status.required_for)}"
    if status.install_hint:
        message += f"; install: {status.install_hint}"
    return message
