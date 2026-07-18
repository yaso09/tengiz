# Plan Document Reviewer Prompt

Review the generated plan document for the following issues:

1. **Spec coverage:** Skim each section/requirement in the spec. Can you point to a task that implements it? List any gaps.

2. **Placeholder scan:** Search for red flags — TBD, TODO, "implement later", "fill in details", "Add appropriate error handling", "add validation", "handle edge cases", "Write tests for the above" (without actual test code), "Similar to Task N" (repeat the code instead).

3. **Type consistency:** Do the types, method signatures, and property names used in later tasks match what was defined in earlier tasks?

If you find issues, flag them for the plan author to fix.
