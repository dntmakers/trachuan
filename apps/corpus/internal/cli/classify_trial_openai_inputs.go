package cli

import (
	"context"
	"fmt"
	"os"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"danny.vn/trachuan/apps/corpus/internal/classify"
	"danny.vn/trachuan/apps/corpus/internal/config"
	"danny.vn/trachuan/apps/corpus/internal/stage/build"
	"danny.vn/trachuan/apps/corpus/internal/taxonomy"
	"github.com/jackc/pgx/v5/pgxpool"
)

func runOpenAITrialDocuments(ctx context.Context, o classify.OpenAITrialOptions, client *classify.OpenAITrialClient, labels taxonomy.Labels) (classify.OpenAITrialSummary, error) {
	var summary classify.OpenAITrialSummary
	if labels.Country != o.Country {
		return summary, fmt.Errorf("labeled set country differs from target release")
	}
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return summary, err
	}
	workshop, err := trialReadPool(ctx, cfg.DatabaseURL, "", "workshop")
	if err != nil {
		return summary, err
	}
	defer workshop.Close()
	name, err := build.DatabaseName(o.Country, o.Release)
	if err != nil {
		return summary, err
	}
	release, err := trialReadPool(ctx, cfg.BuilderDatabaseURL, name, "release")
	if err != nil {
		return summary, err
	}
	defer release.Close()
	if err = release.QueryRow(ctx, `SELECT taxonomy_version FROM release WHERE singleton AND country=$1 AND rel=$2`, o.Country, o.Release).Scan(&o.Recipe.TaxonomyHash); err != nil {
		return summary, fmt.Errorf("read trial release taxonomy: %w", err)
	}
	families, err := classify.LoadFamilies(ctx, release)
	if err != nil {
		return summary, err
	}
	var documents []classify.Document
	if o.Unseeded {
		documents, err = classify.LoadUnseededDocuments(ctx, release, workshop, artifact.Store{Root: cfg.Artifacts}, labels)
	} else {
		documents, err = classify.LoadSelectedDocuments(ctx, release, workshop, artifact.Store{Root: cfg.Artifacts}, labels)
	}
	if err != nil {
		return summary, err
	}
	return classify.RunOpenAIDocumentTrial(ctx, o, client, documents, families)
}

func trialReadPool(ctx context.Context, url, database, purpose string) (*pgxpool.Pool, error) {
	if url == "" {
		return nil, fmt.Errorf("missing %s database configuration", purpose)
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("invalid %s database configuration", purpose)
	}
	if database != "" {
		cfg.ConnConfig.Database = database
	}
	cfg.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	return pgxpool.NewWithConfig(ctx, cfg)
}

func runOpenAITrialArticles(ctx context.Context, o classify.OpenAITrialOptions, client *classify.OpenAITrialClient, labels taxonomy.Labels, familiesFile string) (classify.OpenAITrialSummary, error) {
	var summary classify.OpenAITrialSummary
	evidence, err := loadClassificationEvidence(ctx, o.Country, o.Release, labels)
	if err != nil {
		return summary, err
	}
	if err = classify.ValidateSelection(labels, evidence.articles, evidence.nodes); err != nil {
		return summary, err
	}
	families, hash, err := classify.ReadFamilyArtifact(familiesFile, evidence.taxonomyHash, evidence.nodes)
	if err != nil {
		return summary, err
	}
	if o.Unseeded {
		if err = validateUnseededFamilies(o.LabelsHash, families); err != nil {
			return summary, err
		}
	}
	if o.ExpectedFamiliesHash != "" && o.ExpectedFamiliesHash != hash {
		return summary, fmt.Errorf("trial families changed while reading")
	}
	if families.Recipe.Provider != "openai" || families.Recipe.Model != o.Recipe.Model || families.Recipe.Effort != o.Recipe.Effort {
		return summary, fmt.Errorf("article model and effort must match the OpenAI document recipe")
	}
	o.Recipe, err = classify.NewArticleRecipe(families.Recipe, hash)
	if err != nil {
		return summary, err
	}
	return classify.RunOpenAIArticleTrial(ctx, o, client, evidence.articles, families, evidence.nodes)
}
