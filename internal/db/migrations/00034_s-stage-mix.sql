-- +goose Up
-- Widen the stage_kind CHECK on steps to take 'mix', machine mixing and
-- kneading (ADR-00022). SQLite cannot alter a CHECK, so steps is rebuilt as
-- 00028 rebuilt it, with the two tables that reference it, which would
-- otherwise be emptied by the cascade when the old steps is dropped. Every row
-- is copied unchanged.
CREATE TABLE steps_new (
    id           INTEGER PRIMARY KEY,
    household_id INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    recipe_id    INTEGER NOT NULL,
    component_id INTEGER NOT NULL,
    position     INTEGER NOT NULL,
    -- step_key is recipe.StepID: the name dependencies use, unique per recipe.
    step_key     TEXT NOT NULL CHECK (step_key <> ''),
    text         TEXT NOT NULL DEFAULT '',
    duration_ns  INTEGER NOT NULL DEFAULT 0 CHECK (duration_ns >= 0),
    passive      INTEGER NOT NULL DEFAULT 0 CHECK (passive IN (0, 1)),
    stage_kind   TEXT NOT NULL DEFAULT ''
        CHECK (stage_kind IN ('', 'prep', 'rise', 'rest', 'steep', 'marinate', 'chill', 'bake', 'roast', 'simmer', 'braise', 'cook', 'serve', 'mix')),
    title        TEXT NOT NULL DEFAULT '',
    UNIQUE (component_id, position),
    UNIQUE (recipe_id, step_key),
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, recipe_id) REFERENCES recipes (household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, component_id) REFERENCES components (household_id, id) ON DELETE CASCADE
);
INSERT INTO steps_new (id, household_id, recipe_id, component_id, position, step_key, text, duration_ns, passive, stage_kind, title)
SELECT id, household_id, recipe_id, component_id, position, step_key, text, duration_ns, passive, stage_kind, title
FROM steps;

CREATE TABLE step_dependencies_new (
    household_id       INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    step_id            INTEGER NOT NULL,
    position           INTEGER NOT NULL,
    depends_on_step_id INTEGER NOT NULL,
    PRIMARY KEY (step_id, position),
    CHECK (step_id <> depends_on_step_id),
    FOREIGN KEY (household_id, step_id) REFERENCES steps_new (household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, depends_on_step_id) REFERENCES steps_new (household_id, id) ON DELETE CASCADE
);
INSERT INTO step_dependencies_new (household_id, step_id, position, depends_on_step_id)
SELECT household_id, step_id, position, depends_on_step_id FROM step_dependencies;

CREATE TABLE recipe_equipment_steps_new (
    household_id        INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    recipe_equipment_id INTEGER NOT NULL,
    position            INTEGER NOT NULL,
    step_id             INTEGER NOT NULL,
    PRIMARY KEY (recipe_equipment_id, position),
    FOREIGN KEY (household_id, recipe_equipment_id) REFERENCES recipe_equipment (household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, step_id) REFERENCES steps_new (household_id, id) ON DELETE CASCADE
);
INSERT INTO recipe_equipment_steps_new (household_id, recipe_equipment_id, position, step_id)
SELECT household_id, recipe_equipment_id, position, step_id FROM recipe_equipment_steps;

DROP TABLE recipe_equipment_steps;
DROP TABLE step_dependencies;
DROP TABLE steps;

ALTER TABLE steps_new RENAME TO steps;
ALTER TABLE step_dependencies_new RENAME TO step_dependencies;
ALTER TABLE recipe_equipment_steps_new RENAME TO recipe_equipment_steps;

CREATE INDEX step_dependencies_depends_on ON step_dependencies (depends_on_step_id);

-- +goose Down
-- Rebuild with the narrower CHECK. A 'mix' step has no kind in the older set,
-- so it goes back to unset.
CREATE TABLE steps_new (
    id           INTEGER PRIMARY KEY,
    household_id INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    recipe_id    INTEGER NOT NULL,
    component_id INTEGER NOT NULL,
    position     INTEGER NOT NULL,
    -- step_key is recipe.StepID: the name dependencies use, unique per recipe.
    step_key     TEXT NOT NULL CHECK (step_key <> ''),
    text         TEXT NOT NULL DEFAULT '',
    duration_ns  INTEGER NOT NULL DEFAULT 0 CHECK (duration_ns >= 0),
    passive      INTEGER NOT NULL DEFAULT 0 CHECK (passive IN (0, 1)),
    stage_kind   TEXT NOT NULL DEFAULT ''
        CHECK (stage_kind IN ('', 'prep', 'rise', 'rest', 'steep', 'marinate', 'chill', 'bake', 'roast', 'simmer', 'braise', 'cook', 'serve')),
    title        TEXT NOT NULL DEFAULT '',
    UNIQUE (component_id, position),
    UNIQUE (recipe_id, step_key),
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, recipe_id) REFERENCES recipes (household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, component_id) REFERENCES components (household_id, id) ON DELETE CASCADE
);
INSERT INTO steps_new (id, household_id, recipe_id, component_id, position, step_key, text, duration_ns, passive, stage_kind, title)
SELECT id, household_id, recipe_id, component_id, position, step_key, text, duration_ns, passive, CASE WHEN stage_kind = 'mix' THEN '' ELSE stage_kind END, title
FROM steps;

CREATE TABLE step_dependencies_new (
    household_id       INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    step_id            INTEGER NOT NULL,
    position           INTEGER NOT NULL,
    depends_on_step_id INTEGER NOT NULL,
    PRIMARY KEY (step_id, position),
    CHECK (step_id <> depends_on_step_id),
    FOREIGN KEY (household_id, step_id) REFERENCES steps_new (household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, depends_on_step_id) REFERENCES steps_new (household_id, id) ON DELETE CASCADE
);
INSERT INTO step_dependencies_new (household_id, step_id, position, depends_on_step_id)
SELECT household_id, step_id, position, depends_on_step_id FROM step_dependencies;

CREATE TABLE recipe_equipment_steps_new (
    household_id        INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    recipe_equipment_id INTEGER NOT NULL,
    position            INTEGER NOT NULL,
    step_id             INTEGER NOT NULL,
    PRIMARY KEY (recipe_equipment_id, position),
    FOREIGN KEY (household_id, recipe_equipment_id) REFERENCES recipe_equipment (household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, step_id) REFERENCES steps_new (household_id, id) ON DELETE CASCADE
);
INSERT INTO recipe_equipment_steps_new (household_id, recipe_equipment_id, position, step_id)
SELECT household_id, recipe_equipment_id, position, step_id FROM recipe_equipment_steps;

DROP TABLE recipe_equipment_steps;
DROP TABLE step_dependencies;
DROP TABLE steps;

ALTER TABLE steps_new RENAME TO steps;
ALTER TABLE step_dependencies_new RENAME TO step_dependencies;
ALTER TABLE recipe_equipment_steps_new RENAME TO recipe_equipment_steps;

CREATE INDEX step_dependencies_depends_on ON step_dependencies (depends_on_step_id);
