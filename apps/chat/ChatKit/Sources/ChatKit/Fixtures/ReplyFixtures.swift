/// Replies made up for the pictures of what stands in and under a reply:
/// code with a line longer than a phone is wide, and tables of each shape
/// a table is laid out in.
public extension ChatFixtures {
    /// A short reply, for what stands under it.
    static let short = """
        The basil wants water today, and the mint can wait until **Thursday**.

        Nothing else on the balcony needs you this week.
        """

    /// One paragraph with every kind of link a reply holds, several to a
    /// line: records, pages that open in the browser, and an address to
    /// write to. What a screen reader is told of each is what this is for.
    static let links = """
        The [Q4 budget](substrate://ada.example.com/tasks/task/q4) is in [the planning notes](https://example.com/notes/planning) and \
        in [the board deck](https://example.com/deck); ask [Grace Hopper](substrate://ada.example.com/people/person/grace) or write \
        to [finance](mailto:finance@example.com).
        """

    /// Code with lines longer than a phone is wide, and a block with none.
    static let longLines = """
        The request, on one line:

        ```bash
        curl -sS -X POST "https://substrate.example.com/api/v1/records" -H "Authorization: Bearer $SUBSTRATE_TOKEN" -d '{"kind":"~/tasks/task"}'
        ```

        What it answers:

        ```json
        {"id": "8f2c41d07a", "kind": "~/tasks/task", "version": 1, "properties": {"title": "Water the basil", "due": "2026-11-20"}}
        ```

        And a block no line of which is long:

        ```swift
        let due = try await tasks.due(by: friday)
        print(due.count) // 3
        ```
        """

    /// One small table, for a picture at the largest text size, where a
    /// screen holds little.
    static let smallTable = """
        What is due, by owner:

        | Owner | Due |
        | :--- | ---: |
        | Ada | 3 |
        | Grace | 1 |
        """

    /// Three tables: one that fits as it is, one whose prose wraps to fit,
    /// and one with more columns than a phone has room for, which scrolls
    /// with its first column held. The last has a record's pill in a cell,
    /// inline Markdown, and a column aligned to each side.
    static let tables = """
        What is due, by owner:

        | Owner | Due |
        | :--- | ---: |
        | Ada | 3 |
        | Grace | 1 |

        What each plant wants:

        | Plant | Care |
        | --- | --- |
        | Basil | Water it at the base early in the day, and never let it stand in a wet saucer overnight. |
        | Mint | Repot it once its roots show at the drain holes; it forgives almost anything else. |

        And the week's tasks in full:

        | Task | Owner | Due | State | Estimate | Notes |
        | :--- | :---: | ---: | --- | ---: | --- |
        | [Q4 budget](substrate://ada.example.com/tasks/task/q4) | Ada | Thu | **blocked** | 4 h | Waiting on the actuals from `finance/actuals` |
        | [Offsite agenda](substrate://ada.example.com/tasks/task/offsite) | Grace | Fri | open | 2 h | Two sessions have *no owner* yet |
        | [Reading list](substrate://ada.example.com/tasks/task/reading) | Ada | Fri | ready | 30 min | Send it as it is |
        """
}
