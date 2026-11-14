import type { Declaration } from "./sources"

/** The app's own package as version 2 declares it (apps/tasks/package):
 * what the app reads its own kinds by before the server's implementors
 * answer, and where they do not (a list binds no trait the route asks
 * after). A test holds it to the contract's copy. */
export const ownDeclarations: Declaration[] = [
  {
    identity: "substrate.reamde.dev/tasks/task",
    definition: {
      authority: "substrate.reamde.dev",
      package: "tasks",
      description:
        "Something you have to do, such as Buy milk or Renew passport, with when it is due and how urgent it is. It is in one of your task lists or in none, and stays open until you mark it done or abandon it.",
      purpose: "primary",
      names: {
        singular: "task",
      },
      traits: [
        "substrate.reamde.dev/core/temporal(point: dueAt)",
        "substrate.reamde.dev/core/lifecycle",
      ],
      displayTemplate: "{name}",
      properties: {
        name: {
          type: "string",
          required: true,
          description: "what to do, in one line, such as Buy milk",
        },
        description: {
          type: "markdown",
          description: "the details, as prose",
        },
        url: {
          type: "url",
          description:
            "a link that goes with the task, such as the page to read or the form to fill in",
        },
        list: {
          type: "reference",
          kind: "substrate.reamde.dev/tasks/list",
          mustExist: true,
          onDelete: "cascade",
          description:
            "the task list it is in; absent when it is in none, which Substrate Tasks shows as its inbox",
        },
        position: {
          type: "decimal",
          description:
            'where the task sits among the tasks of its list, lowest first; a number written as a string, such as "2" or "1.5"',
        },
        dueDay: {
          type: "date",
          description:
            "the day it is due; written on every task with a due date, timed or not",
        },
        priority: {
          type: "enum",
          description: "how urgent it is, from none to urgent",
          default: "none",
          values: ["none", "low", "medium", "high", "urgent"],
        },
        status: {
          type: "state",
          description:
            "open until you mark it done or abandon it; either can be undone. A suggested task waits as proposed until you add or dismiss it",
          states: ["proposed", "open", "done", "abandoned"],
          initial: "open",
          done: ["done"],
          dropped: ["abandoned"],
          proposed: ["proposed"],
          transitions: [
            {
              from: "proposed",
              to: "open",
            },
            {
              from: "proposed",
              to: "abandoned",
            },
            {
              from: "open",
              to: "done",
              stamps: {
                completedAt: "now",
              },
            },
            {
              from: "done",
              to: "open",
            },
            {
              from: "open",
              to: "abandoned",
            },
            {
              from: "abandoned",
              to: "open",
            },
          ],
        },
        completedAt: {
          type: "datetime",
          description: "when it was marked done",
        },
      },
    },
  },
  {
    identity: "substrate.reamde.dev/tasks/list",
    label: {
      singular: "Task list",
      plural: "Task lists",
    },
    definition: {
      authority: "substrate.reamde.dev",
      package: "tasks",
      description:
        "A task list: a named list of your tasks, such as Groceries or Home, kept in the order you arrange your lists. A task is in one list or in none.",
      purpose: "primary",
      names: {
        singular: "list",
      },
      label: {
        singular: "Task list",
        plural: "Task lists",
      },
      displayTemplate: "{name}",
      properties: {
        name: {
          type: "string",
          required: true,
          description: "the list's name, such as Groceries",
        },
        position: {
          type: "decimal",
          description:
            'where the list sits among your lists, lowest first; a number written as a string, such as "2" or "1.5"',
        },
      },
    },
  },
  {
    identity: "substrate.reamde.dev/tasks/collection",
    label: {
      singular: "Task collection",
      plural: "Task collections",
    },
    definition: {
      authority: "substrate.reamde.dev",
      package: "tasks",
      description:
        "How Substrate Tasks shows one collection of your tasks: whether it is on, what it groups by, and which of its properties the app reads as the due date, the priority and the order.",
      purpose: "supporting",
      names: {
        singular: "collection",
      },
      label: {
        singular: "Task collection",
        plural: "Task collections",
      },
      displayTemplate: "{kind}",
      properties: {
        kind: {
          type: "string",
          required: true,
          description:
            "the collection's kind, by its full reference, such as substrate.reamde.dev/tasks/task",
        },
        enabled: {
          type: "bool",
          description: "whether Substrate Tasks shows the collection",
        },
        group: {
          type: "string",
          description:
            "the reference the collection's tasks are grouped by; absent for the app's guess, `-` for none",
        },
        due: {
          type: "string",
          description:
            "the property read as the due time; absent for the app's guess, `-` for none",
        },
        dueDay: {
          type: "string",
          description:
            "the property read as the due day; absent for the app's guess, `-` for none",
        },
        priority: {
          type: "string",
          description:
            "the property read as the priority; absent for the app's guess, `-` for none",
        },
        order: {
          type: "string",
          description:
            "the property read as the owner's order; absent for the app's guess, `-` for none",
        },
      },
    },
  },
  {
    identity: "substrate.reamde.dev/tasks/preference",
    label: {
      singular: "Tasks preference",
      plural: "Tasks preferences",
    },
    definition: {
      authority: "substrate.reamde.dev",
      package: "tasks",
      description:
        "Your choices across every collection Substrate Tasks shows: where new tasks go, and the groups you pinned, in your order.",
      purpose: "supporting",
      names: {
        singular: "preference",
      },
      label: {
        singular: "Tasks preference",
        plural: "Tasks preferences",
      },
      properties: {
        newTasks: {
          type: "string",
          description:
            "the kind new tasks are written to, by its full reference; absent for substrate.reamde.dev/tasks/task",
        },
        pins: {
          type: "reference",
          repeated: true,
          description: "the groups you pinned, in your order",
        },
      },
    },
  },
]
