# Folders and linked keys

Folders now determine matrix and sidebar placement even when a key belongs to a key group. Key groups appear as Linked keys, with a separate per-key relationship label and publishing/presence explanations in the editors. Server semantics and API field names remain unchanged.

Regression coverage: a linked key stays under its folder; browser lifecycle selectors use the new terminology. Delivery proceeds through a signed PR and exact-head CI.

Validation: Node 26.7.0 typecheck and production build passed; 684 web unit tests passed, followed by 9 catalogue tests after the error-copy adjustment. Linking/unlinking and catalogue lifecycle browser flows passed on desktop and mobile (2 each). Mobile screenshot is in the ignored web/test-results directory.
