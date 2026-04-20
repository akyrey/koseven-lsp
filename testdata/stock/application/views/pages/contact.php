<?php defined('SYSPATH') or die('No direct script access.'); ?>
<h1>Contact</h1>
<p>Name: <?php echo HTML::chars($name); ?></p>
<p>Subject: <?php echo HTML::chars($subject); ?></p>
<p><?php echo HTML::chars($message); ?></p>
