"""Tests for the SemVer constraint mini-parser."""
from __future__ import annotations

import unittest

from tabula_distro.semver import Constraint, Version, VersionError


class VersionParseTests(unittest.TestCase):
    def test_basic(self):
        v = Version.parse("1.2.3")
        self.assertEqual((v.major, v.minor, v.patch, v.pre), (1, 2, 3, ""))

    def test_v_prefix(self):
        self.assertEqual(Version.parse("v0.8.0"), Version.parse("0.8.0"))

    def test_pre(self):
        v = Version.parse("1.0.0-rc.1")
        self.assertEqual(v.pre, "rc.1")

    def test_invalid(self):
        for bad in ("", "1", "1.2", "1.2.x", "abc"):
            with self.assertRaises(VersionError, msg=bad):
                Version.parse(bad)

    def test_ordering(self):
        self.assertLess(Version.parse("0.8.0"), Version.parse("0.9.0"))
        self.assertLess(Version.parse("0.8.9"), Version.parse("0.9.0"))
        self.assertLess(Version.parse("0.9.0"), Version.parse("1.0.0"))


class ConstraintTests(unittest.TestCase):
    def test_range(self):
        c = Constraint.parse(">=0.8.0,<1.0.0")
        self.assertTrue(c.matches("0.8.0"))
        self.assertTrue(c.matches("0.9.5"))
        self.assertFalse(c.matches("0.7.9"))
        self.assertFalse(c.matches("1.0.0"))

    def test_exact(self):
        c = Constraint.parse("==1.2.3")
        self.assertTrue(c.matches("1.2.3"))
        self.assertFalse(c.matches("1.2.4"))

    def test_bare_is_exact(self):
        c = Constraint.parse("0.8.0")
        self.assertTrue(c.matches("0.8.0"))
        self.assertFalse(c.matches("0.8.1"))

    def test_gt_lte(self):
        c = Constraint.parse(">0.8.0,<=0.9.0")
        self.assertFalse(c.matches("0.8.0"))
        self.assertTrue(c.matches("0.8.1"))
        self.assertTrue(c.matches("0.9.0"))
        self.assertFalse(c.matches("0.9.1"))

    def test_invalid(self):
        with self.assertRaises(VersionError):
            Constraint.parse("")
        with self.assertRaises(VersionError):
            Constraint.parse(">=abc")


if __name__ == "__main__":
    unittest.main()
