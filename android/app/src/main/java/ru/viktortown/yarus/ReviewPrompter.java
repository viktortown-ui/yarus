package ru.viktortown.yarus;

import android.app.Activity;
import ru.rustore.sdk.review.RuStoreReviewManager;
import ru.rustore.sdk.review.RuStoreReviewManagerFactory;

/** Opens the unmodified RuStore review flow. Eligibility and frequency are decided before this call. */
public final class ReviewPrompter {
    private ReviewPrompter() {}

    public static void launch(Activity activity) {
        try {
            RuStoreReviewManager manager = RuStoreReviewManagerFactory.INSTANCE.create(activity);
            manager.requestReviewFlow()
                    .addOnSuccessListener(reviewInfo -> manager.launchReviewFlow(reviewInfo)
                            .addOnSuccessListener(unit -> { /* The user returned to YARUS. */ })
                            .addOnFailureListener(error -> { /* RuStore recommends a silent failure. */ }))
                    .addOnFailureListener(error -> { /* No custom error or substitute prompt. */ });
        } catch (Throwable ignored) {
            // Review availability must never interrupt warehouse work.
        }
    }
}
