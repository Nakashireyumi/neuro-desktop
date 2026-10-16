import copy
import importlib.util
import json
from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("marketplace_validate", ROOT / "validate.py")
VALIDATOR = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(VALIDATOR)


class MarketplaceTests(unittest.TestCase):
    def setUp(self):
        self.index = json.loads((ROOT / "index.json").read_text(encoding="utf-8"))

    def rejected(self, mutate, message):
        changed = copy.deepcopy(self.index)
        mutate(changed)
        with self.assertRaisesRegex(ValueError, message):
            VALIDATOR.validate(changed)

    def test_seed_lists_all_requested_sources_and_does_not_claim_runtime_certification(self):
        self.assertEqual(VALIDATOR.validate(self.index), 8)
        self.assertEqual({item["id"] for item in self.index["items"]}, {
            "neuratro", "neurocompanion", "dont-starve-neuro", "neuro-stardew",
            "neuro-skyrim", "neuromcp", "neurontainer", "neuropilot",
        })
        self.assertTrue(all(not item["platforms"]["tested"] for item in self.index["items"]))
        dont_starve = next(item for item in self.index["items"] if item["id"] == "dont-starve-neuro")
        self.assertEqual(dont_starve["source_ref"], "single")

    def test_ambiguous_identifiers_and_unpinned_instructions_are_rejected(self):
        self.rejected(lambda index: index["items"].append(copy.deepcopy(index["items"][0])), "duplicate item")
        self.rejected(lambda index: index["items"][0].update(id="../escape"), "invalid identifier")
        self.rejected(lambda index: index["items"][0].update(source_commit="main"), "source_commit")
        self.rejected(lambda index: index["items"][0]["install"].update(instructions_url="https://github.com/example/project/blob/main/README.md"), "pinned source commit")

    def test_credentials_and_executable_recipes_cannot_enter_a_listing(self):
        for url in ["http://github.com/example/repo", "https://token@github.com/example/repo", "https://@github.com/example/repo", "https://github.com/example/repo?token=secret", "https://github.com:99999/example/repo"]:
            with self.subTest(url=url):
                self.rejected(lambda index: index["items"][0].update(repository=url), "HTTPS URL")
        self.rejected(lambda index: index["items"][0]["install"].update(command=["sh", "install.sh"]), "unexpected or missing fields")
        self.rejected(lambda index: index["items"][0]["install"].update(mode="managed"), "listings, not managed installers")

    def test_unresolved_license_and_external_permissions_remain_honest(self):
        for item_id in ["neuratro", "neurocompanion"]:
            item = next(item for item in self.index["items"] if item["id"] == item_id)
            self.assertEqual(item["install"]["readiness"], "license-review")
        self.rejected(lambda index: index["items"][0]["install"].update(readiness="upstream-setup-required"), "unresolved license")
        self.rejected(lambda index: index["items"][0]["permissions"].update(enforcement="sandboxed"), "cannot claim to sandbox")
        self.rejected(lambda index: index["items"][0]["platforms"].update(documented=["all"]), "invalid platform")

    def test_schema_and_validator_agree_on_the_seed_when_jsonschema_is_available(self):
        try:
            import jsonschema
        except ImportError:
            self.skipTest("Optional jsonschema package is not installed; the stdlib validator is authoritative")
        schema = json.loads((ROOT / "index.schema.json").read_text(encoding="utf-8"))
        jsonschema.Draft202012Validator.check_schema(schema)
        jsonschema.Draft202012Validator(schema, format_checker=jsonschema.FormatChecker()).validate(self.index)


if __name__ == "__main__":
    unittest.main()
